package fyneui

import (
	"strconv"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// The cluster Overview's health header (Dark Cockpit, design prototype): three bracketed groups in
// the cluster's cyan above the charts, built from what the page's model already holds (no request
// of its own): the nodes, one rounded square each by its state; the pods, bars as wide as their
// count by state (the reference's long bars); and the integrations' bearing (instrument_bearing.go).
// What is healthy is quiet: a legend names only the states that are not zero, and a cluster
// without problems shows mint tokens and nothing else.

// healthLegend is a row of coloured dots with a count and a word each: "● 44 running  ● 2 pending".
type healthLegend struct {
	row *fyne.Container
}

func newHealthLegend() *healthLegend {
	return &healthLegend{row: box(layout.NewCustomPaddedHBoxLayout(14))}
}

// set shows the counts, in the order given; zero counts are left out.
func (l *healthLegend) set(counts []engine.StatusCount) {
	l.row.Objects = l.row.Objects[:0]
	for _, c := range counts {
		if c.Count == 0 {
			continue
		}
		dot := newSurface(statusColorName(c.Status))
		dot.radius, dot.min = theme.SizeNameSelectionRadius, fyne.NewSize(6, 6)
		l.row.Add(box(layout.NewCustomPaddedHBoxLayout(5), newDotSlot(dot),
			newTextLine(strconv.Itoa(c.Count)+" "+healthWord(c.Status), colorMuted, theme.SizeNameCaptionText, fyne.TextStyle{})))
	}
	l.row.Refresh()
}

// newDotSlot centres a status dot of 6 points in a box of the text's height.
func newDotSlot(dot fyne.CanvasObject) fyne.CanvasObject {
	return box(dotLayout{}, dot)
}

type dotLayout struct{}

func (dotLayout) MinSize([]fyne.CanvasObject) fyne.Size { return fyne.NewSize(6, 6) }
func (dotLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	for _, o := range objs {
		o.Move(fyne.NewPos(0, (size.Height-6)/2))
		o.Resize(fyne.NewSize(6, 6))
	}
}

// healthWord is the word of a state in a legend: lowercase, as the bracket labels are.
func healthWord(s model.Status) string {
	switch s {
	case model.StatusOK:
		return "running"
	case model.StatusSucceeded:
		return "succeeded"
	case model.StatusPending:
		return "pending"
	case model.StatusWarning:
		return "warning"
	case model.StatusError:
		return "failed"
	case model.StatusTerminating:
		return "terminating"
	}
	return "unknown"
}

// overviewHealth is the header: the nodes, the pods and the integrations.
type overviewHealth struct {
	nodesHead, podsHead, intHead *bracketLabel
	nodes, pods                  *tokenRow
	nodesLegend, podsLegend      *healthLegend
	bearing                      *bearing
	nodesCol, podsCol, intCol    *fyne.Container
	root                         fyne.CanvasObject
}

// healthNodeSquares is how many node squares the row draws one by one; a larger cluster's nodes
// are bars by state, as the reference coarsens its squares.
const healthNodeSquares = 40

// healthSide is the height of the node and pod rows and the largest a node square gets: the same
// size as the integrations' tiles (instrumentTileSide), so the three groups share one scale.
const healthSide = instrumentTileSide

func newOverviewHealth(sh *shell) *overviewHealth {
	h := &overviewHealth{
		nodesHead: newBracketLabel("nodes", colorAccent), podsHead: newBracketLabel("pods", colorAccent),
		intHead:     newBracketLabel("integrations", colorAccent),
		nodes:       newTokenRow(healthSide),
		pods:        newTokenRow(healthSide),
		nodesLegend: newHealthLegend(), podsLegend: newHealthLegend(),
		bearing: newBearing(sh),
	}
	h.pods.bars = true
	h.nodes.fit, h.nodes.gap = healthSide, 6
	h.pods.gap = 6
	col := func(head, content fyne.CanvasObject, legend fyne.CanvasObject) *fyne.Container {
		return box(layout.NewCustomPaddedVBoxLayout(8), head, content, legend)
	}
	h.nodesCol = col(h.nodesHead, h.nodes, h.nodesLegend.row)
	h.podsCol = col(h.podsHead, h.pods, h.podsLegend.row)
	h.intCol = box(layout.NewCustomPaddedVBoxLayout(8), h.intHead, h.bearing)
	h.root = box(columnsLayout{gap: pageColumnGap}, h.nodesCol, h.podsCol, h.intCol)
	return h
}

// pageColumnGap is the space between the Overview's three columns. The page has one left edge:
// the first column starts on it, with no padding of its own, and so do the title, the controls and
// the charts' titles.
const pageColumnGap = 28

// columnsLayout lays its objects in equal columns with a gap between them and none at the ends,
// so that the first starts on the page's left edge and the last ends on its right.
type columnsLayout struct{ gap float32 }

func (l columnsLayout) MinSize(objs []fyne.CanvasObject) fyne.Size {
	var w, h float32
	n := 0
	for _, o := range objs {
		if !o.Visible() {
			continue
		}
		n++
		ms := o.MinSize()
		w, h = max(w, ms.Width), max(h, ms.Height)
	}
	if n == 0 {
		return fyne.Size{}
	}
	return fyne.NewSize(w*float32(len(objs))+l.gap*float32(len(objs)-1), h)
}

func (l columnsLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	n := float32(len(objs))
	w := (size.Width - l.gap*(n-1)) / n
	for i, o := range objs {
		o.Move(fyne.NewPos(float32(i)*(w+l.gap), 0))
		o.Resize(fyne.NewSize(w, size.Height))
	}
}

// setStates shows the nodes' and pods' states.
func (h *overviewHealth) setStates(nodeStates, podStates []engine.StatusCount) {
	nodeTotal, ready := 0, 0
	var toks []tok
	for _, c := range nodeStates {
		nodeTotal += c.Count
		if c.Status == model.StatusOK {
			ready = c.Count
		}
	}
	h.nodes.bars = nodeTotal > healthNodeSquares
	for _, c := range nodeStates {
		if h.nodes.bars {
			toks = append(toks, tok{color: statusColorName(c.Status), weight: float32(c.Count), quiet: c.Status == model.StatusOK})
			continue
		}
		for range c.Count {
			toks = append(toks, tok{color: statusColorName(c.Status), quiet: c.Status == model.StatusOK})
		}
	}
	h.nodes.SetTokens(toks)
	head := "nodes · " + strconv.Itoa(ready) + " ready"
	if ready < nodeTotal {
		head = "nodes · " + strconv.Itoa(ready) + " of " + strconv.Itoa(nodeTotal) + " ready"
	}
	h.nodesHead.SetText(head)
	h.nodesLegend.set(notHealthy(nodeStates))
	h.nodesCol.Show()
	if nodeTotal == 0 {
		h.nodesCol.Hide()
	}

	podTotal := 0
	toks = toks[:0:0]
	for _, c := range podStates {
		podTotal += c.Count
		toks = append(toks, tok{color: statusColorName(c.Status), weight: float32(c.Count), quiet: c.Status == model.StatusOK})
	}
	h.pods.SetTokens(toks)
	h.podsHead.SetText("pods · " + strconv.Itoa(podTotal))
	h.podsLegend.set(podStates)
	h.podsCol.Show()
	if podTotal == 0 {
		h.podsCol.Hide()
	}
}

// notHealthy is the states of a node list that are not Ready: the legend names those only.
func notHealthy(states []engine.StatusCount) []engine.StatusCount {
	var out []engine.StatusCount
	for _, c := range states {
		if c.Status != model.StatusOK {
			out = append(out, engine.StatusCount{Status: c.Status, Count: c.Count})
		}
	}
	return out
}

// setBearing shows the integrations' tokens.
func (h *overviewHealth) setBearing(items []bearingItem) {
	h.bearing.SetItems(items)
	h.intHead.SetText(bearingHead(items))
}
