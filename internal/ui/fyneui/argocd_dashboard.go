package fyneui

import (
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
)

// The Argo CD Dashboard's upper part (resource-parity › Paid Lens Desktop views › Argo CD): Lens's
// documented health strip (Applications, Synced, Healthy, Out of sync, Degraded, Missing,
// Progressing), each opening the Applications view filtered to its state (D-34), a card per Argo CD
// kind with its count and health bar, the Applications card with Sync and Refresh of several
// Applications, and the recent activity. Needs attention below is the
// page's table. The model is the engine's (argocd.dashboard); the look is Cogpit's own (inferred:
// Lens's dashboard is not captured).

// argoActivityLines bounds the activity the panel lists; agents read 20.
const argoActivityLines = 8

// argoPanel shows the Dashboard's model.
type argoPanel struct {
	sh                      *shell
	data                    *engine.ArgoCDDashboardModel
	strip                   *fyne.Container
	cards                   *fyne.Container
	activity                *fyne.Container
	stripHead, activityHead *bracketLabel // the groups' heads (Dark Cockpit)
	filter                  *searchEntry  // the activity's filter: the window's; F1–F12 are the hotbar's (A64)
	openUI                  *widget.Button
	sync                    *widget.Button
	refresh                 *widget.Button
	root                    fyne.CanvasObject

	requests atomic.Int32 // coalesced as the Workloads Overview's (overview.go)
}

func newArgoPanel(sh *shell) *argoPanel {
	p := &argoPanel{sh: sh}
	p.strip = container.NewGridWithColumns(7)
	p.cards = container.NewGridWithColumns(5)
	p.activity = container.NewVBox()
	p.sync = widget.NewButtonWithIcon("Sync Out of Sync…", theme.UploadIcon(), func() {
		if p.data != nil {
			sh.openArgoSyncDialog(p.data.OutOfSync)
		}
	})
	p.refresh = widget.NewButtonWithIcon("Refresh All…", theme.ViewRefreshIcon(), func() {
		if p.data != nil {
			sh.openArgoRefreshDialog(p.data.All)
		}
	})
	// Lens's "Open Argo UI": a port forward to argocd-server, opened in the browser.
	p.openUI = widget.NewButtonWithIcon("Open Argo CD UI", theme.ComputerIcon(), func() {
		sh.dispatch("argocd.openUI", struct{}{}, func(res any, err error) { sh.forwardDone(res, err, true) })
	})
	p.sync.Disable()
	p.refresh.Disable()
	actions := container.NewHBox(layout.NewSpacer(), p.openUI, p.sync, p.refresh)
	p.stripHead = newBracketLabel("applications", colorAccent)
	p.activityHead = newBracketLabel("recent activity", colorAccent)
	p.filter = newSearchEntry(sh)
	p.filter.SetPlaceHolder("Filter activity…")
	p.filter.OnChanged = func(string) { p.showActivity() }
	head := container.NewBorder(nil, nil, nil, sized(sizeSearchWidth, "", p.filter))
	p.root = inset(theme.SizeNameInnerPadding, theme.SizeNamePadding,
		container.NewVBox(p.stripHead, newSpace(theme.SizeNameInnerPadding), p.strip, newSpace(theme.SizeNamePadding), actions, p.cards,
			gapOf(groupGap), p.activityHead, head, p.activity))
	return p
}

// request reads the model off the main goroutine and shows it, coalesced.
func (p *argoPanel) request() {
	for {
		n := p.requests.Load()
		if n >= 2 {
			return
		}
		if p.requests.CompareAndSwap(n, n+1) {
			if n == 1 {
				return
			}
			break
		}
	}
	p.sh.dispatch("argocd.dashboard", struct{}{}, func(res any, err error) {
		again := p.requests.Swap(0) == 2
		if err == nil {
			var m engine.ArgoCDDashboardModel
			if derr := decode(res, &m); derr == nil {
				p.show(&m)
			}
		}
		if again && p.sh.ui.View == engine.ArgoCDDashboard {
			p.request()
		}
	})
}

// show draws a model.
func (p *argoPanel) show(m *engine.ArgoCDDashboardModel) {
	p.data = m
	if len(m.Strip) > 0 {
		p.stripHead.SetText("applications · " + strconv.Itoa(m.Strip[0].Count))
	}
	tiles := make([]fyne.CanvasObject, 0, len(m.Strip))
	for _, t := range m.Strip {
		tiles = append(tiles, newArgoTile(p.sh, t, m.Synced))
	}
	p.strip.Objects = tiles
	p.strip.Refresh()
	cards := make([]fyne.CanvasObject, 0, len(m.Kinds))
	for _, k := range m.Kinds {
		cards = append(cards, newArgoCard(p.sh, k))
	}
	p.cards.Objects = cards
	p.cards.Refresh()
	p.enable(p.sync, len(m.OutOfSync) > 0)
	p.enable(p.refresh, len(m.All) > 0)
	p.showActivity()
}

// showActivity lists the newest activities that match the filter (the Application's namespace and
// name, what happened, the revision; every term, ignoring case), at most argoActivityLines.
func (p *argoPanel) showActivity() {
	if p.data == nil {
		return
	}
	terms := strings.Fields(strings.ToLower(p.filter.Text))
	lines := make([]fyne.CanvasObject, 0, argoActivityLines)
	now := p.sh.now()
	for _, a := range p.data.Activity {
		if len(lines) == argoActivityLines {
			break
		}
		text := strings.ToLower(a.Ref.Key() + " " + a.Text + " " + a.Revision)
		if !slices.ContainsFunc(terms, func(t string) bool { return !strings.Contains(text, t) }) {
			lines = append(lines, newArgoActivityLine(p.sh, a, now))
		}
	}
	switch {
	case len(lines) > 0:
	case len(terms) > 0:
		lines = append(lines, newTextLine("No activity matches the filter.", colorMuted, theme.SizeNameText, fyne.TextStyle{}))
	default:
		lines = append(lines, newTextLine("No operations yet.", colorMuted, theme.SizeNameText, fyne.TextStyle{}))
	}
	p.activity.Objects = lines
	p.activity.Refresh()
}

func (p *argoPanel) enable(b *widget.Button, on bool) {
	if on {
		b.Enable()
	} else {
		b.Disable()
	}
}

// newArgoTile is one count of the strip: the count large, in its status colour when not zero, the
// label below; a click opens the Applications view filtered to the tile's state (its status filter,
// D-34: "Sync: OutOfSync", "Health: Degraded"), Applications unfiltered.
func newArgoTile(sh *shell, t engine.ArgoTile, synced bool) fyne.CanvasObject {
	// Quiet when healthy (Dark Cockpit): the number is the text colour, a zero is muted, and only a
	// count of something that needs a look is in its status colour with a spine at the tile's edge.
	color := theme.ColorNameForeground
	lit := t.Count > 0 && t.Status != model.StatusNone && t.Status != model.StatusOK && t.Status != model.StatusSucceeded
	switch {
	case lit:
		color = statusColorName(t.Status)
	case t.Count == 0:
		color = colorMuted
	}
	count := formatCount(int64(t.Count))
	if !synced {
		count += " …"
	}
	num := newTextLine(count, color, theme.SizeNameHeadingText, fyne.TextStyle{Bold: true})
	label := newLinkText(t.Label, func() { sh.openArgoApplications(t.State) })
	plate := newSurface(theme.ColorNameInputBackground)
	plate.radius = theme.SizeNameDialogRadius
	body := inset(theme.SizeNameInnerPadding, theme.SizeNamePadding, container.NewVBox(num, label))
	if !lit {
		return container.NewStack(plate, body)
	}
	spine := newSurface(statusColorName(t.Status))
	spine.radius, spine.min = theme.SizeNameSelectionRadius, fyne.NewSize(3, 0)
	return container.NewStack(plate, container.NewBorder(nil, nil, inset("", theme.SizeNameInnerPadding, spine), nil, body))
}

// openArgoApplications opens Argo CD's Applications view filtered to a state of its status filter
// ("" unfiltered), in one ui.openView.
func (sh *shell) openArgoApplications(state string) {
	p := openViewParams{View: kinds.ArgoApplicationsKey}
	if state != "" {
		p.States = []string{state}
	}
	sh.dispatch("ui.openView", p, func(_ any, err error) { sh.showError(err) })
}

// newArgoCard is one kind's card: its title as a link to its view, its count, its health bar.
func newArgoCard(sh *shell, k engine.OverviewKind) fyne.CanvasObject {
	t := newOverviewTile(sh, k.Kind)
	t.set(k)
	return t
}

// newArgoActivityLine is one entry of the recent activity: how long ago, the Application (a link
// to it), what happened in its status colour, the revision.
func newArgoActivityLine(sh *shell, a engine.ArgoActivityItem, now time.Time) fyne.CanvasObject {
	// Quiet when healthy: the sentence is text and muted colour; only a dot and the age carry a
	// failure's or a wait's colour.
	tone := statusColorName(a.Status)
	healthy := a.Status == model.StatusOK || a.Status == model.StatusSucceeded || a.Status == model.StatusNone
	ageTone := tone
	if healthy {
		ageTone = colorMuted
	}
	ago := newTextLine(model.FormatAge(now.Sub(a.At))+" ago", ageTone, theme.SizeNameText, fyne.TextStyle{})
	ref := a.Ref
	app := newLinkText(ref.Name, func() { sh.dispatch("ui.navigate", refParams{Ref: ref}, nil) })
	text := a.Text
	if a.Revision != "" {
		text += " · " + a.Revision
	}
	what := newTextLine(text, theme.ColorNameForeground, theme.SizeNameText, fyne.TextStyle{})
	what.truncate = true
	dotted := fyne.CanvasObject(newDotSlot(newSurfaceDot(tone)))
	if healthy {
		dotted = newDotSlot(newSurfaceDot(colorTokenDim))
	}
	return container.NewBorder(nil, nil, container.NewHBox(dotted, ago, box(naturalWidth{text: ref.Name}, app)), nil, what)
}

// naturalWidth lays out a link (which takes any width, shortening its text) as wide as its text.
type naturalWidth struct{ text string }

func (n naturalWidth) MinSize([]fyne.CanvasObject) fyne.Size {
	return fyne.MeasureText(n.text, fyne.CurrentApp().Settings().Theme().Size(theme.SizeNameText), fyne.TextStyle{})
}

func (n naturalWidth) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	for _, o := range objs {
		o.Move(fyne.NewPos(0, 0))
		o.Resize(size)
	}
}

// newSurfaceDot is a status dot of 6 points: the colour of a state, drawn as a small rounded plate.
func newSurfaceDot(c fyne.ThemeColorName) *surface {
	d := newSurface(c)
	d.radius, d.min = theme.SizeNameSelectionRadius, fyne.NewSize(6, 6)
	return d
}
