package fyneui

import (
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"

	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
)

// An Ingress's Rules in the drawer (D-137): each rule's host once, across the details' width, a
// link to the browser; under it the rule's paths, a row each: the path, a link to the host's
// address with the path; its type, muted; an arrow and the backend, whose Service's name is a link
// to the Service and whose port follows as text. The paths of all hosts share their columns. A web
// link's tooltip is its address, and its copy icon copies the address; the backend's copy icon is
// its port's and copies the whole backend. The section is the kind's table (Host, Path, Type,
// Backend), which agents read as it is; its head counts the rules (DetailSection.Count).

func init() { drawerSections["ingresses\x00"+kinds.TitleIngressRules] = ingressRulesSection }

// rulePathParts is the number of objects a path's row has in rulePathsLayout.
const rulePathParts = 5

func ingressRulesSection(d *drawer, ref model.ObjectRef, sec model.DetailSection) fyne.CanvasObject {
	out := container.NewVBox()
	t := sec.Table
	if t == nil {
		return out
	}
	cols := &ruleColumns{room: d.copyBtn.MinSize().Width}
	var paths []fyne.CanvasObject
	flush := func() {
		if len(paths) > 0 {
			out.Add(box(&rulePathsLayout{cols: cols}, paths...))
			cols.all = append(cols.all, paths...)
			paths = nil
		}
	}
	host := ""
	for r, row := range t.Cells {
		hostCell, hostText := tableCellAt(t, row, r, 0)
		if r == 0 || hostText != host {
			flush()
			if r > 0 {
				out.Add(newSpace(theme.SizeNamePadding))
			}
			out.Add(d.ruleHost(hostCell, hostText))
			host = hostText
		}
		pathCell, pathText := tableCellAt(t, row, r, 1)
		_, typ := tableCellAt(t, row, r, 2)
		backendCell, backendText := tableCellAt(t, row, r, 3)
		name, rest := d.ruleBackend(ref, backendCell, backendText)
		paths = append(paths, d.webText(pathCell, pathText),
			d.line(typ, colorMuted, theme.SizeNameText, fyne.TextStyle{}, true, typ),
			d.line("→", colorMuted, theme.SizeNameText, fyne.TextStyle{}, false, ""), name, rest)
	}
	flush()
	return out
}

// webAddress is the address of a cell of one web address, "" for none.
func webAddress(c model.Cell) string {
	if c.Web == nil || len(*c.Web) != 1 {
		return ""
	}
	return (*c.Web)[0].URL
}

// ruleHost is a rule's host on a line of its own: a link to its address, wrapped onto lines when
// it is wider than the details (nameLink), or muted text for "*" and a wildcard host.
func (d *drawer) ruleHost(c model.Cell, text string) fyne.CanvasObject {
	addr := webAddress(c)
	if addr == "" {
		return d.line(text, colorMuted, theme.SizeNameText, fyne.TextStyle{}, true, text)
	}
	o := nameLink(text, d.contentWidth(), func() { d.sh.openWebLink(addr) })
	lines := []fyne.CanvasObject{o}
	if c, ok := o.(*fyne.Container); ok {
		lines = c.Objects
	}
	for i, line := range lines {
		l := line.(*linkText)
		l.tips, l.tip = d.sh.tip, addr
		if i == 0 { // a wrapped host's copy icon is its first line's
			d.hookLink(l, addr)
		}
	}
	return o
}

// webText is a cell's text as a link to its web address, else as a selectable line.
func (d *drawer) webText(c model.Cell, text string) fyne.CanvasObject {
	addr := webAddress(c)
	if addr == "" {
		return d.line(text, theme.ColorNameForeground, theme.SizeNameText, fyne.TextStyle{}, true, text)
	}
	l := newLinkText(text, func() { d.sh.openWebLink(addr) })
	d.hookLink(l, addr)
	l.tips, l.tip = d.sh.tip, addr
	return l
}

// ruleBackend is a backend's text in two parts: the link to its object, when the cluster serves the
// object's kind, and the rest of the text after it. A Service's link is its name, the port the
// rest; a resource's link is the whole text. Without a link the text is all in the second part.
func (d *drawer) ruleBackend(ref model.ObjectRef, c model.Cell, text string) (link, rest fyne.CanvasObject) {
	plain := func(s, copied string) fyne.CanvasObject {
		return d.line(s, theme.ColorNameForeground, theme.SizeNameText, fyne.TextStyle{}, true, copied)
	}
	if c.Link == nil || text == "" || !d.sh.view.serves(c.Link.Kind) {
		return plain("", ""), plain(text, text)
	}
	name, after := text, ""
	if strings.HasPrefix(text, c.Link.Name) {
		name, after = text[:len(c.Link.Name)], text[len(c.Link.Name):]
	}
	to := *c.Link
	return newLinkText(name, func() { d.sh.followLink(to, ref.Context, false) }), plain(after, text)
}

// ruleColumns are what the paths of all of a section's hosts share: their objects, which their
// columns are measured over, and the room the copy icon takes after a path.
type ruleColumns struct {
	all  []fyne.CanvasObject
	room float32
}

// rulePathsLayout lays out a rule's paths under its host, indented, rulePathParts objects a row:
// the path, its type, the arrow, and the backend's link with the rest of its text right after it.
// The path, the type and the arrow are columns as wide as their widest text over all hosts, the
// path's with room for its copy icon, so that the icon shortens no path; the backend takes the
// rest. When a row does not fit, the widest columns give up width first (capWidths) and their
// texts shorten, a backend's rest before its link.
type rulePathsLayout struct{ cols *ruleColumns }

// columns are the widths of the path, the type, the arrow and the backend, and the gap after each
// of the first three.
func (l *rulePathsLayout) columns(width float32) (w [4]float32, gaps [3]float32) {
	th := fyne.CurrentApp().Settings().Theme()
	size, pad := th.Size(theme.SizeNameText), th.Size(theme.SizeNamePadding)
	objs := l.cols.all
	for i := 0; i+rulePathParts <= len(objs); i += rulePathParts {
		for c := range 3 {
			w[c] = fyne.Max(w[c], cellTextWidth(objs[i+c], size))
		}
		w[3] = fyne.Max(w[3], cellTextWidth(objs[i+3], size)+cellTextWidth(objs[i+4], size))
	}
	w[0] += pad + l.cols.room
	gaps = [3]float32{pad, 3 * pad, pad}
	if avail := width - l.indent() - w[2] - gaps[0] - gaps[1] - gaps[2]; w[0]+w[1]+w[3] > avail {
		capped := []float32{w[0], w[1], w[3]}
		capWidths(capped, fyne.Max(avail, 0))
		w[0], w[1], w[3] = capped[0], capped[1], capped[2]
	}
	return w, gaps
}

func (l *rulePathsLayout) indent() float32 {
	return fyne.CurrentApp().Settings().Theme().Size(theme.SizeNameInnerPadding)
}

func (l *rulePathsLayout) rowHeight(objs []fyne.CanvasObject) (h, gap float32) {
	for _, o := range objs {
		h = fyne.Max(h, o.MinSize().Height)
	}
	return h, fyne.CurrentApp().Settings().Theme().Size(theme.SizeNamePadding)
}

func (l *rulePathsLayout) MinSize(objs []fyne.CanvasObject) fyne.Size {
	rows := float32(len(objs) / rulePathParts)
	if rows == 0 {
		return fyne.Size{}
	}
	h, gap := l.rowHeight(objs)
	return fyne.NewSize(0, rows*h+(rows-1)*gap)
}

func (l *rulePathsLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	w, gaps := l.columns(size.Width)
	h, gap := l.rowHeight(objs)
	textSize := fyne.CurrentApp().Settings().Theme().Size(theme.SizeNameText)
	place := func(o fyne.CanvasObject, x, y, width float32) {
		o.Move(fyne.NewPos(x, y))
		o.Resize(fyne.NewSize(fyne.Max(width, 0), h))
	}
	y := float32(0)
	for i := 0; i+rulePathParts <= len(objs); i += rulePathParts {
		x := l.indent()
		for c := range 3 {
			place(objs[i+c], x, y, w[c])
			x += w[c] + gaps[c]
		}
		link := fyne.Min(cellTextWidth(objs[i+3], textSize), w[3])
		place(objs[i+3], x, y, link)
		place(objs[i+4], x+link, y, w[3]-link)
		y += h + gap
	}
}
