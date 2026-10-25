package fyneui

import (
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/helm"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/ui/fyneui/yamlview"
)

// Chips of a short list share one line; a longer list shows one chip per line.
const (
	chipsPerLine     = 6
	chipsLineMaxText = 64 // the most characters of chip text on one line
)

// drawer is the detail drawer, after Lens's (capture, 2026-10-11): the object's kind and name in
// the title bar with its actions as icons (the registry's actions for the kind, as in the row
// menu), then the Properties section, the kind's own sections and the events. Cogpit's YAML view
// is a second tab. It refreshes when the engine reports the object changed.
type drawer struct {
	sh    *shell
	width float32 // the drawer's width the details were built for (contentWidth)

	shown       *model.ObjectRef
	last        *engine.DetailModel
	acts        []actionState
	busy, dirty bool
	expanded    map[string]bool   // folded lists the user opened, by foldKey
	revealed    map[string]string // values item actions show in place (a Secret's), by revealKey (secretreveal.go)
	revealedLen map[string]int    // how many bytes of a revealed text are the value; the rest is a note that it was cut
	kept        map[string]any    // what section renderers keep across rebuilds for the shown object (drawerSections)

	// A Secret's edited values the drawer moved away from, by object, in memory only (secretdata.go).
	secretUnsaved map[string]*secUnsaved

	kind     *bracketLabel // the eyebrow: "pod · inventory" (stage_header.go)
	title    *textLine
	status   *statusBadge
	wash     *stateWash // the header's backdrop, tinted by the object's status
	missing  *textLine
	lead     *fyne.Container // a kind's own part left of the title (drawerHeaderParts); hidden without
	extra    *fyne.Container // a kind's own part under the title (drawerHeaderParts); hidden without
	actions  *fyne.Container // the action icons in the title bar
	nameCopy *tipButton      // Copy, right after the title (drawertitlecopy.go)
	titleBox *fyne.Container // the title and nameCopy
	head     *fyne.Container // the header and the line under it (layoutHeader)
	frame    *fyne.Container // head above body
	tabs     *container.AppTabs
	body     *fyne.Container // the tabs, or for a plain drawer (Lens's release drawer) the details alone
	pane     *fyne.Container // the details' place in the first tab
	scroll   *container.Scroll
	plain    bool
	details  *fyne.Container
	host     *fyne.Container // details with the copy icon above them (copy.go)
	yaml     *yamlview.View
	find     *findBar // over the YAML for good: it is read-only (newViewerFindBar)
	yamlTab  *container.TabItem
	yamlText string
	root     fyne.CanvasObject

	hostLayout *overlayLayout
	copyBtn    *copyButton
	copyOwner  fyne.CanvasObject // the value the copy icon is shown for
	copyHook   *copyHook
	copyDone   *time.Timer       // the checkmark's timer; nil when none shows
	copyStale  fyne.CanvasObject // the value it was shown for before the last rebuild
	copyHooks  []*copyHook       // the hooks of the values built last (keepCopy)
}

func newDrawer(sh *shell) *drawer {
	d := &drawer{sh: sh, expanded: map[string]bool{}, revealed: map[string]string{}, revealedLen: map[string]int{}, kept: map[string]any{}}
	d.kind = newBracketLabel("", colorAccent)
	d.wash = newStateWash()
	d.title = newTextLine("", theme.ColorNameForeground, theme.SizeNameHeadingText, fyne.TextStyle{Bold: true})
	d.title.truncate = true
	d.status = newStatusBadge()
	d.status.Hide()
	d.missing = newTextLine("This object is no longer in the cluster.", colorStatusWarning, theme.SizeNameCaptionText, fyne.TextStyle{})
	d.missing.Hide()
	d.actions = container.NewHBox()
	d.nameCopy = newTipButton(sh, commandIcon(copyNameID), "Copy", nil)
	d.nameCopy.Hide()
	d.titleBox = box(&titleCopyLayout{title: d.title}, d.title, d.nameCopy)

	closeB := newTipButton(sh, theme.CancelIcon(), "Close", sh.closeDrawer)

	d.details = container.NewVBox()
	d.copyBtn = newCopyButton(d)
	d.hostLayout = &overlayLayout{}
	d.host = box(d.hostLayout, d.details, d.copyBtn)
	d.yaml = yamlview.New()
	d.yaml.SetReadOnly(true)
	d.find = newViewerFindBar(sh, d.yaml)
	d.yamlTab = container.NewTabItem("YAML", container.NewBorder(d.find.root, nil, nil, nil, d.yaml))
	d.scroll = newVScroll(inset(theme.SizeNameInnerPadding, theme.SizeNameInnerPadding, d.host))
	d.pane = container.NewStack(d.scroll)
	d.tabs = container.NewAppTabs(
		container.NewTabItem("Details", d.pane),
		d.yamlTab,
	)
	d.body = container.NewStack(d.tabs)

	d.lead, d.extra = container.NewStack(), container.NewStack()
	d.lead.Hide()
	d.extra.Hide()
	// The name keeps the row's width: the status stands on a line of its own under it, so that a long
	// name is not cut by the badge (the name is the object's identity; the status is read second).
	titleRow := container.NewBorder(nil, nil, d.lead,
		container.NewHBox(d.actions, newDivider(true), closeB),
		d.titleBox)
	header := container.NewStack(d.wash,
		inset(theme.SizeNameInnerPadding, theme.SizeNameInnerPadding,
			container.NewVBox(d.kind, titleRow, container.NewHBox(d.status), d.extra, d.missing)))
	d.head = container.NewVBox(header, newDivider(false))
	d.frame = container.NewBorder(d.head, nil, nil, nil, d.body)
	// The root's minimum width is a constant: the drawer's width is the user's (panelwidths.go).
	d.root = box(lockedWidth{}, newSurface(colorDrawer), d.frame)
	d.width = clampWidth(sh.drawerW, minDrawerWidth, maxDrawerWidth, minDrawerWidth)
	d.status.limit(d.statusWidth())
	return d
}

// contentWidth is the width of the details: the drawer's width less its padding and the scroll
// bar. The widgets of the details are built to it, so that none asks for more than the drawer has
// (long values wrap or are cut), and they are built again when the drawer's width changes.
func (d *drawer) contentWidth() float32 {
	th := d.sh.app.Settings().Theme()
	return max(d.width-2*th.Size(theme.SizeNameInnerPadding)-th.Size(theme.SizeNameScrollBar)-th.Size(theme.SizeNamePadding), 0)
}

// valueWidth is the width of a value beside its name in a Properties-style grid (kvLayout).
func (d *drawer) valueWidth() float32 {
	th := d.sh.app.Settings().Theme()
	return max(d.contentWidth()-th.Size(sizeDrawerLabel)-2*th.Size(theme.SizeNamePadding), 0)
}

// statusWidth is the most the status pill in the title bar may take: the title and the icons
// keep the rest.
func (d *drawer) statusWidth() float32 { return d.width * 0.8 }

// widthChanged runs after a layout gave the drawer another width (a drag, the window's size): the
// widgets that were built to the old width are built again to the new one.
func (d *drawer) widthChanged() {
	w := d.sh.drawerLay.got
	if w == d.width || w <= 0 {
		return
	}
	d.width = w
	d.status.limit(d.statusWidth())
	d.rebuild()
}

// request fetches the drawer object's detail and evaluates its actions off the main goroutine,
// then shows them. Requests that arrive while one runs collapse into one more fetch afterwards.
func (d *drawer) request() {
	ref := d.sh.ui.Drawer
	if ref == nil {
		return
	}
	if d.busy {
		d.dirty = true
		return
	}
	d.busy = true
	r := *ref
	if d.shown == nil || *d.shown != r {
		d.showLoading(r)
	}
	go func() {
		var (
			m    *engine.DetailModel
			err  error
			acts []actionState
		)
		func() {
			defer func() {
				if p := recover(); p != nil {
					err = fmt.Errorf("internal error: %v", p)
				}
			}()
			m, err = d.sh.eng.Detail(r)
			var containers []string
			if m != nil {
				containers = podContainers(m) // rowmenu.go
			}
			acts = d.sh.evaluateActions(r, containers)
		}()
		d.sh.do(func() {
			d.busy = false
			if cur := d.sh.ui.Drawer; cur != nil && *cur == r {
				d.show(r, m, err, acts)
			}
			if d.dirty {
				d.dirty = false
				d.request()
			}
		})
	}()
}

// showLoading retargets the drawer at a new object while its detail loads.
func (d *drawer) showLoading(ref model.ObjectRef) {
	r := ref
	d.shown = &r
	d.last = nil
	d.acts = nil
	d.expanded = map[string]bool{}
	d.revealed = map[string]string{}
	d.revealedLen = map[string]int{}
	d.dropKept()
	d.showHeader(ref, nil)
	d.resetCopy()
	d.setPlain(ref.Kind == helm.ReleaseKind)
	d.kind.SetText(eyebrow(singular(d.sh.kindTitle(ref.Kind)), ref.Namespace))
	d.wash.Set("", 0)
	d.setTitle(ref.Name)
	d.status.Hide()
	d.missing.Hide()
	d.actions.Objects = nil
	d.actions.Refresh()
	d.nameCopy.Hide()
	d.layoutHeader()
	d.details.Objects = []fyne.CanvasObject{newTextLine("Loading…", colorMuted, theme.SizeNameText, fyne.TextStyle{})}
	d.details.Refresh()
	d.setYAML("", false)
	d.tabs.SelectIndex(0)
}

// layoutHeader lays the header out again at once after showLoading or show changed what it holds
// (the action icons, the status, the lines under the title), and the body below it at the header's
// height. Fyne's GL driver lays a parent out before a frame only when a child's minimum size
// differs from the one the last frame saw. A drawer moved to another pod is laid out while it loads
// (drawerChanged refreshes the workspace after showLoading), and the pod's detail, read from the
// cache, often comes before the next frame: with icons as wide as the last pod's and a status as
// tall, no frame saw a change, and the header kept the loading layout, the icons drawn from the
// close button on past the drawer's edge and the status under the tabs (the owner's finding of
// 2026-10-22). The header holds a few widgets; a refresh of it costs little.
func (d *drawer) layoutHeader() {
	if s := d.frame.Size(); s.Width <= 0 || s.Height <= 0 {
		return // not laid out yet: its first layout places everything
	}
	d.frame.Layout.Layout(d.frame.Objects, d.frame.Size())
	d.head.Refresh()
}

// setPlain switches between the drawer with Details and YAML tabs and Lens's flat release drawer
// (captured 2026-10-12): the title "Release: <name>" on one line, no tabs, no status pill beside the
// icons (the Status property is a pill of its own).
func (d *drawer) setPlain(plain bool) {
	if plain == d.plain {
		return
	}
	d.plain = plain
	if plain {
		d.pane.Objects = nil
		d.body.Objects = []fyne.CanvasObject{d.scroll}
		d.kind.Hide()
	} else {
		d.pane.Objects = []fyne.CanvasObject{d.scroll}
		d.body.Objects = []fyne.CanvasObject{d.tabs}
		d.kind.Show()
	}
	d.pane.Refresh()
	d.body.Refresh()
}

// show renders a detail model. Unchanged sections are not rebuilt, so that a churning object does
// not flicker or lose the scroll position.
func (d *drawer) show(ref model.ObjectRef, m *engine.DetailModel, err error, acts []actionState) {
	if d.shown == nil || *d.shown != ref {
		d.showLoading(ref)
	}
	if err != nil || m == nil {
		msg := "No details."
		if err != nil {
			msg = "Could not load the details: " + err.Error()
		}
		d.details.Objects = []fyne.CanvasObject{newTextLine(msg, colorStatusError, theme.SizeNameText, fyne.TextStyle{})}
		d.details.Refresh()
		return
	}
	d.setPlain(m.Plain)
	kind, name := splitTitle(m.Title)
	if kind == "" {
		kind = singular(d.sh.kindTitle(ref.Kind))
	}
	d.kind.SetText(eyebrow(kind, ref.Namespace))
	d.setTitle(name)
	if m.Plain {
		d.setTitle(m.Title) // "Release: web" on one line
	}
	if label := statusLabel(m); label != "" && m.Status != model.StatusNone && !m.Plain {
		d.status.Set(label, statusColorName(m.Status))
		d.status.Show()
	} else {
		d.status.Hide()
	}
	if m.Plain {
		d.wash.Set("", 0)
	} else {
		d.wash.setStatus(m.Status)
	}
	if m.Missing {
		d.missing.Show()
	} else {
		d.missing.Hide()
	}
	d.showHeader(ref, m)
	d.showActions(ref, acts)
	d.layoutHeader()

	prev := d.last
	d.last = m
	if prev == nil || !reflect.DeepEqual(prev.Sections, m.Sections) || !reflect.DeepEqual(prev.Events, m.Events) ||
		prev.Metrics != m.Metrics { // the cluster's settings hid or showed the Metrics section
		d.rebuild()
	}
	// The same object changed in the cache: keep the reader's place in the YAML. Another object:
	// start at the top.
	if !m.Plain {
		d.setYAML(m.YAML, prev != nil && prev.Ref.Same(m.Ref))
	}
}

// rebuild renders the shown model's sections and events again.
func (d *drawer) rebuild() {
	if d.last == nil {
		return
	}
	anchor := d.copyAnchorNow()
	d.resetCopy()
	d.details.Objects = d.buildDetails(d.last)
	d.details.Refresh()
	d.keepCopy(anchor)
}

// clear forgets the shown object when the drawer closes.
func (d *drawer) clear() {
	d.shown, d.last, d.acts, d.dirty = nil, nil, nil, false
	d.revealed = map[string]string{}
	d.revealedLen = map[string]int{}
	d.dropKept()
	d.resetCopy()
	d.sh.tip.hide()
}

// setYAML hands the YAML to the viewer only when it changed. For an update of the shown object it
// keeps the scroll position, caret and selection (UpdateText); SetText would jump to the top on
// every watch event.
func (d *drawer) setYAML(text string, sameObject bool) {
	if text == d.yamlText {
		return
	}
	d.yamlText = text
	if sameObject {
		d.yaml.UpdateText(text)
	} else {
		d.yaml.SetText(text)
	}
	d.yaml.Refresh()
	d.find.textChanged()
}

// yamlShown reports whether the drawer is open on its YAML tab, whose find bar ⌘F then opens.
func (d *drawer) yamlShown() bool {
	return d.sh.ui.Drawer != nil && d.root.Visible() && d.tabs.Selected() == d.yamlTab
}

// showActions puts the object's actions in the title bar as icons, in the row menu's order (Lens:
// a pod's seven icons end with Edit and Delete; a node shows Shell, Cordon or Uncordon, Drain,
// Edit, Delete). The tooltip names the action and, for a disabled one, says why.
func (d *drawer) showActions(ref model.ObjectRef, acts []actionState) {
	if len(d.actions.Objects) > 0 && sameActions(d.acts, acts) {
		return // a churning object keeps its buttons, and a hovered button its tooltip
	}
	d.acts = acts
	d.actions.Objects = d.actions.Objects[:0]
	d.nameCopy.Hide()
	for _, a := range acts {
		if a.cmd.ID == copyNameID {
			d.showNameCopy(ref, a)
			continue
		}
		b := newTipButton(d.sh, commandIcon(a.cmd.ID), actionTip(a), nil)
		b.OnTapped = func() { d.run(ref, a, b) }
		if a.reason != "" {
			b.Disable()
		}
		d.actions.Add(b)
	}
	d.actions.Refresh()
}

// sameActions reports whether two evaluations of an object's actions show the same toolbar.
func sameActions(a, b []actionState) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].cmd != b[i].cmd || a[i].reason != b[i].reason || a[i].dialog != b[i].dialog ||
			strings.Join(a[i].containers, ",") != strings.Join(b[i].containers, ",") || string(a[i].prefill) != string(b[i].prefill) {
			return false
		}
	}
	return true
}

// run runs an action from the toolbar. A container command on a pod with several containers asks
// which, in a menu below the icon.
func (d *drawer) run(ref model.ObjectRef, a actionState, from fyne.CanvasObject) {
	if len(a.containers) <= 1 || a.dialog {
		first := ""
		if len(a.containers) == 1 {
			first = a.containers[0]
		}
		d.sh.runAction(a, ref, first)
		return
	}
	items := make([]*fyne.MenuItem, 0, len(a.containers))
	for _, c := range a.containers {
		items = append(items, fyne.NewMenuItem(c, func() { d.sh.runAction(a, ref, c) }))
	}
	if cnv := fyne.CurrentApp().Driver().CanvasForObject(from); cnv != nil {
		widget.ShowPopUpMenuAtRelativePosition(fyne.NewMenu(a.cmd.Label, items...), cnv,
			fyne.NewPos(0, from.Size().Height), from)
	}
}

// buildDetails renders the sections in their order, a group heading ("Containers") above the
// first section of each group, then the events.
func (d *drawer) buildDetails(m *engine.DetailModel) []fyne.CanvasObject {
	d.pruneRevealed(m)
	out := make([]fyne.CanvasObject, 0, 4*len(m.Sections)+4)
	panel := d.metricsPanel(m) // a pod's or node's Metrics section comes first, as in Lens
	if panel != nil {
		out = append(out, d.metricsSection(panel)...)
		out = append(out, newSpace(theme.SizeNameInnerPadding))
	}
	group := ""
	for i, sec := range m.Sections {
		if i > 0 {
			out = append(out, newSpace(theme.SizeNameInnerPadding))
		}
		if sec.Group != "" && sec.Group != group {
			out = append(out, newSectionHead(sec.Group, groupSize(m.Sections, sec.Group)),
				newSpace(theme.SizeNamePadding))
		}
		group = sec.Group
		out = append(out, d.sectionTitle(sec))
		if sec.Group != "" {
			out = append(out, newDivider(false)) // a container under its group's bracket keeps a rule of its own
		} else {
			out = append(out, newSpace(theme.SizeNamePadding))
		}
		if panel != nil && containerTitleHasChart(sec) {
			if c := panel.containerChart(sec.Title); c != nil {
				out = append(out, c, newSpace(theme.SizeNamePadding))
			}
		}
		if render, ok := drawerSections[m.Ref.Kind+"\x00"+sec.Title]; ok && sec.Group == "" {
			out = append(out, render(d, m.Ref, sec))
			continue
		}
		// A section with both (a node's Devices: a slice's pool and link, then its devices) shows
		// its items first.
		if len(sec.Items) > 0 {
			grid := box(kvLayout{})
			for _, it := range sec.Items {
				grid.Objects = append(grid.Objects, d.itemName(it.Name), d.itemValue(m.Ref, sec, it))
			}
			out = append(out, grid)
		}
		if sec.Table != nil {
			out = append(out, d.table(m.Ref, sec.Title, sec.Table))
		}
		if sec.Grade != nil { // a container's image grade, under its items (drawergrade.go)
			out = append(out, newSpace(theme.SizeNamePadding), newDivider(false), newSpace(theme.SizeNamePadding),
				d.gradeCard(m.Ref, sec.Grade))
		}
	}
	if m.NoEvents { // a kind no event is about (Helm releases): Lens's drawer has no Events
		return out
	}
	out = append(out, newSpace(theme.SizeNameInnerPadding),
		newSectionHead("Events", len(m.Events)), newSpace(theme.SizeNamePadding))
	return append(out, d.buildEvents(m.Events)...)
}

// itemName is an item's name in the details' name column (kvLayout): one line, or, for a name
// wider than the column ("Pod Management Policy", "Volume Claim Templates" of a Stateful Set),
// wrapped onto lines within it, so that it never runs into its value: at its spaces, a word without
// them after - . _ / and, failing those, by characters (wrapName; the owner's F3, 2026-10-15:
// "agent-not-ready-taint-key" ran under its value). A name that fits is drawn as before.
func (d *drawer) itemName(text string) fyne.CanvasObject {
	th := d.sh.app.Settings().Theme()
	if w := th.Size(sizeDrawerLabel); fyne.MeasureText(text, th.Size(theme.SizeNameText), fyne.TextStyle{}).Width > w {
		return newNameParagraph(text, colorMuted, w)
	}
	return d.line(text, colorMuted, theme.SizeNameText, fyne.TextStyle{}, false, "")
}

// drawerSections are the GUI's own renderings of drawer sections, by kind and section title: a
// section listed here is drawn by its function instead of as items or a table (a release's values
// in an editor, helmdrawer.go). Each feature registers its renderer in an init of its own file. A
// renderer may keep widgets in drawer.kept across rebuilds of the same object's sections.
var drawerSections = map[string]func(d *drawer, ref model.ObjectRef, sec model.DetailSection) fyne.CanvasObject{}

// drawerHeaderParts are the GUI's own parts of a kind's drawer header, by kind: what shows left of the
// title and under it (a Helm chart's icon and version picker, helmchartdrawer.go); nil hides the
// place, so that other kinds' headers stay as they are. A function may keep its widgets in
// drawer.kept; it gets nil while the object loads.
var drawerHeaderParts = map[string]func(d *drawer, m *engine.DetailModel) (lead, extra fyne.CanvasObject){}

// showHeader shows the kind's own parts of the header for m (none for nil).
func (d *drawer) showHeader(ref model.ObjectRef, m *engine.DetailModel) {
	var lead, extra fyne.CanvasObject
	if f := drawerHeaderParts[ref.Kind]; f != nil && m != nil {
		lead, extra = f(d, m)
	}
	setHeaderSlot(d.lead, lead)
	setHeaderSlot(d.extra, extra)
}

// setHeaderSlot shows o in c, or hides c for nil.
func setHeaderSlot(c *fyne.Container, o fyne.CanvasObject) {
	if o == nil {
		if len(c.Objects) > 0 {
			c.Objects = nil
			c.Refresh()
		}
		c.Hide()
		return
	}
	if len(c.Objects) != 1 || c.Objects[0] != o {
		c.Objects = []fyne.CanvasObject{o}
		c.Refresh()
	}
	c.Show()
}

// keptCloser is something a section renderer keeps that must stop (timers, loads) when the drawer
// moves to another object or closes.
type keptCloser interface{ close() }

// dropKept forgets what section renderers kept for the shown object.
func (d *drawer) dropKept() {
	for _, k := range d.kept {
		if c, ok := k.(keptCloser); ok {
			c.close()
		}
	}
	d.kept = map[string]any{}
}

// sectionTitle is a section's bold title; a container's carries a dot in its status's colour and,
// with the Trivy integration on, its image's grade as a shield after its name (drawergrade.go).
func (d *drawer) sectionTitle(sec model.DetailSection) fyne.CanvasObject {
	if sec.Group == "" {
		return sectionHeadOf(sec)
	}
	title := d.line(sec.Title, theme.ColorNameForeground, theme.SizeNameText, fyne.TextStyle{Bold: true}, false, "")
	objs := []fyne.CanvasObject{title}
	for _, it := range sec.Items {
		if it.Name == "Status" && it.Status != model.StatusNone {
			objs = []fyne.CanvasObject{container.NewCenter(newDot(statusColorName(it.Status))), title}
			break
		}
	}
	if sec.Grade != nil {
		objs = append(objs, d.titleGrade(sec.Grade))
	}
	if len(objs) == 1 {
		return title
	}
	return container.NewHBox(objs...)
}

// foldKey names a folded list for the expanded state, which survives refreshes of the object.
func foldKey(sec model.DetailSection, it model.DetailItem) string {
	return sec.Group + "\x00" + sec.Title + "\x00" + it.Name
}

// itemValue renders one value: a folded list as its count ("6 Labels", a link that unfolds it),
// chips, a link to the object it names, a status pill, else one line per line. An item's actions
// follow the value as buttons; an action marked Link is the value itself, a link that runs it. A
// Secret's values, whose Show reveals them, are its Data section's (secretdata.go).
func (d *drawer) itemValue(ref model.ObjectRef, sec model.DetailSection, it model.DetailItem) fyne.CanvasObject {
	if len(it.Actions) == 0 {
		return d.foldedValue(ref, sec, it, true)
	}
	var v fyne.CanvasObject
	hooks := len(d.copyHooks) // the value's own copy hooks follow
	objs := []fyne.CanvasObject{nil}
	for _, a := range it.Actions {
		act := a
		if act.Link && v == nil {
			l := newLinkText(it.Value, func() { d.sh.runItemAction(ref, act) })
			d.hookLink(l, it.Value)
			v = l
			continue
		}
		b := newTextButton(act.Label, nil, func() { d.sh.runItemAction(ref, act) })
		objs = append(objs, b)
	}
	if v == nil {
		v = d.foldedValue(ref, sec, it, true)
	}
	objs[0] = v
	natural, icon := float32(0), float32(0)
	if it.Fold == "" {
		size := d.sh.app.Settings().Theme().Size(theme.SizeNameText)
		for _, line := range strings.Split(it.Value, "\n") {
			natural = fyne.Max(natural, fyne.MeasureText(line, size, fyne.TextStyle{}).Width)
		}
	}
	switch {
	case len(objs) > 1:
		// The owner's decision (A71, 2026-10-14 evening): the buttons (a port's Forward…) right
		// after the value, as before 2026-10-13, and the copy icon over the value's end on hover,
		// as in the lists (A69): the value keeps no room for it and its text is not shortened.
		for _, h := range d.copyHooks[hooks:] {
			h.over = true
		}
	case it.Value != "":
		// No buttons (a forward's URL, a link that runs its action): the value owns the room for
		// its icon right after its text, as a chip does.
		icon = d.copyBtn.MinSize().Width + d.sh.app.Settings().Theme().Size(theme.SizeNamePadding)
	}
	return box(actionRowLayout{natural: natural, icon: icon}, objs...)
}

// actionRowLayout puts a drawer value and its action buttons on one line: the value as wide as
// its text (natural; 0 takes what the buttons leave) and, without buttons, the room for its copy
// icon (icon), shortened when the drawer is narrower, the buttons right after it. The value's own
// widgets ask for no width, since they shorten to fit.
type actionRowLayout struct{ natural, icon float32 }

func (actionRowLayout) gap() float32 {
	return fyne.CurrentApp().Settings().Theme().Size(theme.SizeNamePadding)
}

func (l actionRowLayout) MinSize(objs []fyne.CanvasObject) fyne.Size {
	var w, h float32
	for _, o := range objs {
		ms := o.MinSize()
		w += ms.Width + l.gap()
		h = fyne.Max(h, ms.Height)
	}
	return fyne.NewSize(w, h)
}

func (l actionRowLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	if len(objs) == 0 {
		return
	}
	var buttons float32
	for _, b := range objs[1:] {
		buttons += b.MinSize().Width + l.gap()
	}
	vw := fyne.Max(size.Width-buttons, 0)
	if l.natural > 0 {
		vw = fyne.Min(vw, l.natural+l.gap()+l.icon)
	}
	vh := objs[0].MinSize().Height
	objs[0].Move(fyne.NewPos(0, (size.Height-vh)/2))
	objs[0].Resize(fyne.NewSize(vw, vh))
	x := vw + l.gap()
	for _, b := range objs[1:] {
		ms := b.MinSize()
		b.Move(fyne.NewPos(x, (size.Height-ms.Height)/2))
		b.Resize(ms)
		x += ms.Width + l.gap()
	}
}

// foldedValue renders a value, folded behind its count when the item folds.
func (d *drawer) foldedValue(ref model.ObjectRef, sec model.DetailSection, it model.DetailItem, copyable bool) fyne.CanvasObject {
	if it.Fold != "" {
		key := foldKey(sec, it)
		n := it.Entries()
		noun := it.Fold
		if n == 1 {
			noun = singular(noun)
		}
		toggle := newLinkText(strconv.Itoa(n)+" "+noun, func() {
			d.expanded[key] = !d.expanded[key]
			d.rebuild()
		})
		if !d.expanded[key] {
			return toggle
		}
		return container.NewVBox(toggle, d.plainValue(ref, it, copyable))
	}
	return d.plainValue(ref, it, copyable)
}

// plainValue renders a value without folding. Its copy icon (copyable) copies the value, or, for a
// value of several entries, the entry the pointer is over, in YAML syntax (D-25).
func (d *drawer) plainValue(ref model.ObjectRef, it model.DetailItem, copyable bool) fyne.CanvasObject {
	lines := strings.Split(it.Value, "\n")
	list := entryList(len(it.Chips), len(lines), it.Fold != "")
	copyOf := func(entry string) string {
		switch {
		case !copyable || entry == "":
			return ""
		case list:
			return yamlEntry(entry)
		}
		return entry
	}
	switch {
	case len(it.Chips) > 0:
		return d.chips(ref, it.Chips, copyOf)
	case it.Link != nil && d.sh.view.serves(it.Link.Kind):
		link := *it.Link
		l := newLinkText(it.Value, func() { d.sh.followLink(link, ref.Context, false) })
		d.hookLink(l, copyOf(it.Value))
		return l
	case it.Pill:
		return wrapCopy(container.NewHBox(d.valuePill(it.Value, colorMuted, true)), d.copyFor(copyOf(it.Value), false))
	case it.Status != model.StatusNone:
		return wrapCopy(container.NewHBox(d.statusValuePill(it.Value, it.Status)), d.copyFor(copyOf(it.Value), false))
	}
	if len(lines) == 1 {
		return d.line(it.Value, theme.ColorNameForeground, theme.SizeNameText, fyne.TextStyle{}, true, copyOf(it.Value))
	}
	v := container.NewVBox()
	for _, l := range lines {
		v.Add(d.line(l, theme.ColorNameForeground, theme.SizeNameText, fyne.TextStyle{}, true, copyOf(l)))
	}
	return v
}

// valuePill is a pill for a value in the details: as wide as its text up to the width a value has
// beside its name, then wrapped (the text stays whole in the copy icon and the selection).
func (d *drawer) valuePill(text string, fg fyne.ThemeColorName, tinted bool) *pill {
	p := newPill(text, fg, tinted)
	p.limit(d.valueWidth())
	return p
}

// statusValuePill is a pill for a status in the details. A healthy one is a muted chip with plain
// text, as in the lists, where a healthy word draws no colour; only a state that asks for
// attention is tinted in its status colour.
func (d *drawer) statusValuePill(text string, s model.Status) *pill {
	return d.valuePill(text, statusPillInk(s), !healthyStatus(s))
}

// healthyStatus reports whether a status is one of the two that need no colour: Running and its
// kin, and Completed.
func healthyStatus(s model.Status) bool {
	return s == model.StatusOK || s == model.StatusSucceeded
}

// statusPillInk is the colour of a status pill's word: the status colour, or for a healthy status
// the plain text colour.
func statusPillInk(s model.Status) fyne.ThemeColorName {
	if healthyStatus(s) {
		return theme.ColorNameForeground
	}
	return statusColorName(s)
}

// chips renders a list as chips: a short list on one line, a longer one a chip per line. A chip
// with a status is tinted in its colour; one with a link goes to the object. A list that does
// not fit the value's width on one line takes a line per chip, and a chip wider than the width
// wraps.
func (d *drawer) chips(ref model.ObjectRef, chips []model.Chip, copyOf func(string) string) fyne.CanvasObject {
	total := 0
	var natural float32
	size := d.sh.app.Settings().Theme().Size(theme.SizeNameCaptionText)
	for _, c := range chips {
		total += len(c.Text)
		natural += fyne.MeasureText(c.Text, size, fyne.TextStyle{}).Width + 3*d.sh.app.Settings().Theme().Size(theme.SizeNamePadding)
	}
	gutter := d.copyBtn.MinSize().Width + d.sh.app.Settings().Theme().Size(theme.SizeNamePadding)
	natural += float32(len(chips)) * gutter // room for the copy icon after each chip
	oneLine := len(chips) <= chipsPerLine && total <= chipsLineMaxText && natural <= d.valueWidth()
	out := container.NewVBox()
	line := container.NewHBox()
	for _, c := range chips {
		var o fyne.CanvasObject
		switch {
		case c.Link != nil && d.sh.view.serves(c.Link.Kind):
			link := *c.Link
			p := d.valuePill(c.Text, colorLink, false)
			p.limit(max(d.valueWidth()-gutter, 0))
			o = newTapTarget(p, func() { d.sh.followLink(link, ref.Context, false) })
		case c.Status != model.StatusNone:
			p := d.statusValuePill(c.Text, c.Status)
			p.limit(max(d.valueWidth()-gutter, 0))
			o = p
		default:
			p := d.valuePill(c.Text, theme.ColorNameForeground, false)
			p.limit(max(d.valueWidth()-gutter, 0))
			o = p
		}
		o = wrapCopy(o, d.copyFor(copyOf(c.Text), false))
		if hw, ok := o.(*hoverWrap); ok {
			// Every chip owns the room for its icon, the last of a row and one alone on its line
			// too: the icon sits after the chip, and the pointer on its way there must stay in
			// the chip's hover area.
			hw.gutter = gutter
		}
		if oneLine {
			line.Add(o)
		} else {
			out.Add(container.NewHBox(o))
		}
	}
	if oneLine {
		return line
	}
	return out
}

// detailTable renders a plain table, its texts only: the column titles muted above the rows, each
// column as wide as its widest line (detailTableLayout), a cell of several lines one line under
// the other. Tables of cells (links, statuses, warning icons) are drawn by table.
func (d *drawer) detailTable(t *model.DetailTable) fyne.CanvasObject {
	if len(t.Columns) == 0 {
		return container.NewVBox()
	}
	// The cells go into the grid at once: Container.Add lays the grid out on every call.
	objs := make([]fyne.CanvasObject, 0, len(t.Columns)*(len(t.Rows)+1))
	for _, c := range t.Columns {
		objs = append(objs, d.line(c, colorMuted, theme.SizeNameText, fyne.TextStyle{}, true, ""))
	}
	for _, row := range t.Rows {
		for i := range t.Columns {
			text := ""
			if i < len(row) {
				text = row[i]
			}
			objs = append(objs, d.tableCellText(text, theme.ColorNameForeground))
		}
	}
	return box(&detailTableLayout{cols: len(t.Columns)}, objs...)
}

// tableCellText is a table cell of text in colour fg: a truncating, selectable line per line of its
// text, each with a copy icon.
func (d *drawer) tableCellText(text string, fg fyne.ThemeColorName) fyne.CanvasObject {
	lines := strings.Split(text, "\n")
	cell := func(s string) *selLine {
		return d.line(s, fg, theme.SizeNameText, fyne.TextStyle{}, true, s)
	}
	if len(lines) == 1 {
		return cell(text)
	}
	v := container.NewVBox()
	for _, l := range lines {
		v.Add(cell(l))
	}
	return v
}

// detailTableLayout lays out table cells (textLines, linkTexts, and boxes of textLines for cells of
// several lines) row by row in cols columns, each as wide as its widest line and each row as tall
// as its tallest cell, rows of several lines further apart; when the columns do not fit, the
// widest give up width first (capWidths) and their texts shorten.
//
// It keeps its last measurement while the objects and the text size stay the same: a table's
// cells never change once built (the drawer builds a new table instead), and Fyne asks for MinSize
// and Layout several times per pass, which for a node's hundreds of pods means thousands of texts.
type detailTableLayout struct {
	cols int

	measured    []fyne.CanvasObject
	textSize    float32
	w, rowH     []float32
	gap, rowGap float32
}

// widths measures the cells: each column's width, each row's height, the gap between columns and
// the one between rows. Callers may change the widths they get.
func (l *detailTableLayout) widths(objs []fyne.CanvasObject) (w, rowH []float32, gap, rowGap float32) {
	th := fyne.CurrentApp().Settings().Theme()
	size := th.Size(theme.SizeNameText)
	if len(objs) > 0 && len(objs) == len(l.measured) && &objs[0] == &l.measured[0] && size == l.textSize {
		return slices.Clone(l.w), l.rowH, l.gap, l.rowGap
	}
	defer func() {
		l.measured, l.textSize, l.w, l.rowH, l.gap, l.rowGap = objs, size, slices.Clone(w), rowH, gap, rowGap
	}()
	gap = th.Size(theme.SizeNamePadding) * 3
	rowGap = gap / 3
	w = make([]float32, l.cols)
	rowH = make([]float32, (len(objs)+l.cols-1)/l.cols)
	var measure func(o fyne.CanvasObject) float32
	measure = func(o fyne.CanvasObject) float32 {
		switch t := o.(type) {
		case *textLine:
			return fyne.MeasureText(t.text, size, t.style).Width
		case *selLine:
			return fyne.MeasureText(t.line.text, size, t.line.style).Width
		case *linkText:
			return fyne.MeasureText(t.text, size, fyne.TextStyle{}).Width
		case *fyne.Container:
			widest := float32(0)
			for _, c := range t.Objects {
				widest = fyne.Max(widest, measure(c))
			}
			return widest
		}
		return o.MinSize().Width // an icon
	}
	for i, o := range objs {
		w[i%l.cols] = fyne.Max(w[i%l.cols], measure(o))
		rowH[i/l.cols] = fyne.Max(rowH[i/l.cols], o.MinSize().Height)
	}
	if len(rowH) > 1 && slices.Max(rowH[1:]) > rowH[0] { // a row of several lines
		rowGap = gap * 2 / 3
	}
	return w, rowH, gap, rowGap
}

func (l *detailTableLayout) MinSize(objs []fyne.CanvasObject) fyne.Size {
	_, rowH, _, rowGap := l.widths(objs)
	h := float32(0)
	for _, rh := range rowH {
		h += rh + rowGap
	}
	return fyne.NewSize(0, h)
}

func (l *detailTableLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	w, rowH, gap, rowGap := l.widths(objs)
	total := gap * float32(l.cols-1)
	for _, x := range w {
		total += x
	}
	if total > size.Width && total > 0 {
		capWidths(w, fyne.Max(size.Width-gap*float32(l.cols-1), 0))
	}
	y := float32(0)
	for i, o := range objs {
		col, row := i%l.cols, i/l.cols
		if col == 0 && row > 0 {
			y += rowH[row-1] + rowGap
		}
		x := float32(0)
		for c := 0; c < col; c++ {
			x += w[c] + gap
		}
		o.Move(fyne.NewPos(x, y))
		o.Resize(fyne.NewSize(w[col], o.MinSize().Height)) // at the top of a taller row
	}
}

// capWidths makes column widths fit avail by capping the widest at one width: columns narrower
// than their share keep their width, so a short column (a rule's verbs) is never cut because a
// long one (its API groups) is too wide.
func capWidths(w []float32, avail float32) {
	if len(w) == 0 {
		return
	}
	sorted := slices.Clone(w)
	slices.Sort(sorted)
	limit := sorted[len(sorted)-1]
	remaining := avail
	for i, x := range sorted {
		if share := remaining / float32(len(sorted)-i); x > share {
			limit = share
			break
		}
		remaining -= x
	}
	for i := range w {
		w[i] = fyne.Min(w[i], limit)
	}
}

// buildEvents renders the object's events, newest first, at the end of the drawer as Lens does;
// "No events found" when there are none (capture, 2026-10-11).
func (d *drawer) buildEvents(events []engine.EventItem) []fyne.CanvasObject {
	if len(events) == 0 {
		return []fyne.CanvasObject{newTextLine("No events found", colorMuted, theme.SizeNameText, fyne.TextStyle{})}
	}
	now := d.sh.now()
	out := make([]fyne.CanvasObject, 0, 2*len(events))
	for _, e := range events {
		st := e.Status
		if st == model.StatusNone {
			st = model.StatusOK
			if e.Type == "Warning" {
				st = model.StatusWarning
			}
		}
		var meta []string
		if e.Source != "" {
			meta = append(meta, e.Source)
		}
		if e.Count > 1 {
			meta = append(meta, fmt.Sprintf("×%d", e.Count))
		}
		if e.LastSeen > 0 {
			meta = append(meta, model.FormatAge(now.Sub(time.Unix(e.LastSeen, 0)))+" ago")
		}
		head := box(eventHead{},
			newPill(e.Type, statusColorName(st), true),
			d.line(e.Reason, theme.ColorNameForeground, theme.SizeNameText, fyne.TextStyle{Bold: true}, true, e.Reason),
			d.line(strings.Join(meta, " · "), colorMuted, theme.SizeNameCaptionText, fyne.TextStyle{}, true, ""))
		msg := widget.NewLabel(e.Message)
		msg.Wrapping = fyne.TextWrapWord
		msg.Selectable = true
		out = append(out, container.NewVBox(head, wrapCopy(msg, d.copyFor(e.Message, false))), newDivider(false))
	}
	return out
}

// eventHead lays out an event's first line: the type pill, the reason beside it and the source,
// count and age at the right. It asks for the pill's width only; when the line is wider than the
// drawer, the meta text keeps at most 45% of what the pill leaves and the reason the rest, and
// both are cut (the drawer's width is the user's, panelwidths.go). Objects: pill, reason, meta.
type eventHead struct{}

func (eventHead) gap() float32 {
	return fyne.CurrentApp().Settings().Theme().Size(theme.SizeNamePadding) * 2
}

func (eventHead) natural(o fyne.CanvasObject) float32 {
	if s, ok := o.(*selLine); ok {
		return fyne.MeasureText(s.line.text, fyne.CurrentApp().Settings().Theme().Size(s.line.size), s.line.style).Width
	}
	return o.MinSize().Width
}

func (l eventHead) MinSize(objs []fyne.CanvasObject) fyne.Size {
	var h float32
	for _, o := range objs {
		h = fyne.Max(h, o.MinSize().Height)
	}
	return fyne.NewSize(objs[0].MinSize().Width, h)
}

func (l eventHead) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	pill, reason, meta := objs[0], objs[1], objs[2]
	g := l.gap()
	pw := pill.MinSize().Width
	avail := fyne.Max(size.Width-pw-2*g, 0)
	rw, mw := l.natural(reason), l.natural(meta)
	if rw+mw > avail {
		mw = fyne.Min(mw, avail*0.45)
		rw = fyne.Min(rw, avail-mw)
	}
	place := func(o fyne.CanvasObject, x, w float32) {
		h := o.MinSize().Height
		o.Move(fyne.NewPos(x, (size.Height-h)/2))
		o.Resize(fyne.NewSize(w, h))
	}
	place(pill, 0, pw)
	place(reason, pw+g, rw)
	place(meta, size.Width-mw, mw)
}

// splitTitle splits "Pod: web-0" into "Pod" and "web-0".
func splitTitle(t string) (kind, name string) {
	if k, n, ok := strings.Cut(t, ": "); ok {
		return k, n
	}
	return "", t
}

// statusLabel is the drawer's status pill text: the Status item of the first section that has
// one, else nothing (a node's state is its Conditions).
func statusLabel(m *engine.DetailModel) string {
	for _, s := range m.Sections {
		for _, it := range s.Items {
			if it.Name == "Status" && it.Value != "" {
				return it.Value
			}
		}
	}
	return ""
}

// singular turns a plural noun into its singular: "Pods" → "Pod", "Ingresses" → "Ingress",
// "Policies" → "Policy", "volumes" → "volume".
func singular(title string) string {
	switch {
	case strings.HasSuffix(title, "sses"):
		return strings.TrimSuffix(title, "es")
	case strings.HasSuffix(title, "ies"):
		return strings.TrimSuffix(title, "ies") + "y"
	case strings.HasSuffix(title, "s"):
		return strings.TrimSuffix(title, "s")
	}
	return title
}

// kvLayout lays out name and value pairs in two columns: names in the theme's label width,
// values in the rest. Objects alternate name, value.
type kvLayout struct{}

func (kvLayout) dims() (nameW, gap float32) {
	th := fyne.CurrentApp().Settings().Theme()
	return th.Size(sizeDrawerLabel), th.Size(theme.SizeNamePadding) * 2
}

func (l kvLayout) MinSize(objs []fyne.CanvasObject) fyne.Size {
	nameW, gap := l.dims()
	var h, valueW float32
	for i := 0; i+1 < len(objs); i += 2 {
		n, v := objs[i].MinSize(), objs[i+1].MinSize()
		h += fyne.Max(n.Height, v.Height) + gap/2
		valueW = fyne.Max(valueW, v.Width)
	}
	return fyne.NewSize(nameW+gap+valueW, h)
}

func (l kvLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	nameW, gap := l.dims()
	y := float32(0)
	for i := 0; i+1 < len(objs); i += 2 {
		n, v := objs[i].MinSize(), objs[i+1].MinSize()
		rowH := fyne.Max(n.Height, v.Height)
		objs[i].Move(fyne.NewPos(0, y))
		objs[i].Resize(fyne.NewSize(nameW, n.Height))
		objs[i+1].Move(fyne.NewPos(nameW+gap, y))
		objs[i+1].Resize(fyne.NewSize(fyne.Max(size.Width-nameW-gap, 0), v.Height))
		y += rowH + gap/2
	}
}

// newSpace is vertical empty space of a theme size.
func newSpace(size fyne.ThemeSizeName) fyne.CanvasObject {
	return sized("", size)
}

// newHGap is horizontal empty space of a theme size.
func newHGap(size fyne.ThemeSizeName) fyne.CanvasObject {
	return sized(size, "")
}
