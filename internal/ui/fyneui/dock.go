package fyneui

import (
	"context"
	"fmt"
	"image/color"
	"io"
	"slices"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/ui/fyneui/termview"
)

// dock is the bottom panel: a tab strip (tabs from UIState.Dock, the terminal chip after them,
// terminalbutton.go; the create chip and a collapse toggle at its right end) above the active
// tab's content. Whether it is open or collapsed to its strip is engine state,
// UIState.DockOpen (ui.toggleDock, ⌘J, the top bar's layout button; engine/layout.go): a new or
// activated tab opens it, and a dock without tabs is closed. Log tabs show termview.Logs on Session.OpenLogs with their
// options' toolbar (logtab.go); terminal, shell and attach tabs show termview.Terminal on
// Session.OpenTerminal; Edit and Create resource tabs show a YAML editor whose text lives in the
// engine (editortab.go, createtab.go). A stream is opened when its tab appears and closed when it
// disappears. A tab dragged along the strip moves there (dock.move; stripdrag.go, the owner's GW4).
//
// The dock per cluster (D-69 item 9, engine/dockscope.go): the strip shows the shown cluster's
// tabs and those of no cluster (UIState.DockInScope); every other connected cluster's tabs keep
// their content and their streams here, hidden, and show again with what ran meanwhile when their
// cluster is shown. Which tab is active and whether the dock is open are the engine's, per cluster.
type dock struct {
	sh *shell

	tabs     []engine.DockTab // the tabs the strip shows: the shown cluster's and those of no cluster
	active   string
	contents map[string]*dockContent // every tab's content, a hidden cluster's too

	tabRow       *fyne.Container
	strip        fyne.CanvasObject
	stripHolder  *fyne.Container // the strip's place on top of the open panel
	collapsedBar *fyne.Container // the strip's place while the dock is collapsed
	body         *fyne.Container
	panel        fyne.CanvasObject
	toggle       *widget.Button
	terminal     *stripChip // the chip after the tabs that opens a terminal tab (terminalbutton.go)
	// agentRead is the strip's "Agents may read" switch of the active terminal tab (dockoutput.go):
	// the tab it shows and since when.
	agentRead        *widget.Check
	syncingAgentRead bool
	agentReadTab     string
	agentReadSince   time.Time
	// agentReadMarks is the tab marker and its flash at each agent's read (dockagentread.go).
	agentReadMarks agentReadMarks
	// drag is a tab dragged to another place in the strip (stripdrag.go, GW4).
	drag *stripDrag
	// focusReq is the tab the window asked the keys for, until its main widget takes them
	// (dockfocus.go, AW5, AW8).
	focusReq *focusRequest
	// menuModel is the tab menu shown last, for tests (tabmenu.go).
	menuModel *fyne.Menu
	// keysLast: a dock tab was clicked after the tab bar's last click or key, so that Move Tab's
	// keys move the dock's tab while nothing is focused: the clicked tab takes no keys, or not yet
	// (tabmovekeys.go, AW10).
	keysLast bool
}

// dockContent is one tab's content and the stream behind it.
type dockContent struct {
	tab    engine.DockTab
	obj    *fyne.Container
	cancel context.CancelFunc
	closer io.Closer
	editor dockEditor // Edit and Create resource tabs
	logs   *logTab    // logs tabs (logtab.go)
	// kubectl is a terminal tab's kubectl line (kubectloffer.go).
	kubectl *kubectlOffer
	// term is a terminal, shell, attach or node shell tab's terminal once its session started,
	// which dock.read reads (dockoutput.go).
	term *termview.Terminal
	// reconnect is a shell, attach or node shell tab's row while the connection's interruption
	// ended its session (dockreconnect.go, D-58).
	reconnect *reconnectRow
	closed    bool
}

// dockEditor is an editor tab's content: it follows EventEditor and stops its timers on close.
type dockEditor interface {
	remote(rev uint64)
	close()
}

func newDock(sh *shell) *dock {
	d := &dock{sh: sh, contents: map[string]*dockContent{}}
	d.drag = newStripDrag(nil, sh.reduceMotion)
	d.tabRow = container.New(&stripLayout{d: d.drag})
	d.drag.row = d.tabRow
	d.drag.idOf = func(o fyne.CanvasObject) string {
		if b, ok := o.(*tabButton); ok {
			return b.id
		}
		return ""
	}
	d.drag.shownIDs = func() []string {
		ids := make([]string, len(d.tabs))
		for i, t := range d.tabs {
			ids[i] = t.ID
		}
		return ids
	}
	d.drag.move = func(id string, index int, failed func()) {
		sh.dispatch("dock.move", dockMoveParams{ID: id, Index: index}, func(_ any, err error) {
			if err != nil {
				sh.showError(err)
				failed()
			}
		})
	}
	d.drag.click = func(o fyne.CanvasObject, at fyne.Position) {
		b, ok := o.(*tabButton)
		switch {
		case !ok:
		case pressedOn(b.close, at):
			b.close.OnTapped()
		case b.onTap != nil:
			b.onTap()
		}
	}
	d.terminal = newTerminalButton(sh)
	d.toggle = widget.NewButtonWithIcon("", theme.MenuDropDownIcon(), func() { d.setOpen(nil) })
	d.toggle.Importance = widget.LowImportance
	d.agentRead = d.newAgentReadSwitch()
	d.agentReadMarks = newAgentReadMarks()
	right := container.NewHBox(d.agentReadMarks.note, d.agentRead, d.toggle)
	if sh.view != nil { // the list's create chip lives here, never over the rows (createbutton.go)
		right = container.NewHBox(container.NewCenter(sh.view.create), d.agentReadMarks.note, d.agentRead, d.toggle, newHGap(theme.SizeNameInnerPadding))
	}
	d.strip = container.NewStack(newSurface(colorDock),
		container.NewBorder(nil, nil, container.NewHBox(d.tabRow, container.NewCenter(d.terminal)), right))
	d.stripHolder = container.NewStack()
	d.collapsedBar = container.NewVBox()
	d.body = container.NewStack()
	// The line above the strip is the handle's over the open dock (dockLayout), as the side panels'.
	d.panel = container.NewBorder(container.NewVBox(d.stripHolder, newDivider(false)), nil, nil, nil,
		container.NewStack(newSurface(colorDock), d.body))
	d.updateToggle()
	return d
}

// open reports whether the dock shows its panel, not only its strip (UIState.DockOpen).
func (d *dock) open() bool { return d.sh.ui.DockOpen && len(d.tabs) > 0 }

// setOpen opens (true) or collapses (false) the dock through the engine, nil the other way round.
func (d *dock) setOpen(open *bool) {
	d.sh.dispatch("ui.toggleDock", layoutToggleParams{Open: open}, nil)
}

// attachStrip moves the strip to the top of the panel, or to the collapsed bar.
func (d *dock) attachStrip(panel bool) {
	if panel {
		d.collapsedBar.Objects = nil
		d.stripHolder.Objects = []fyne.CanvasObject{d.strip}
	} else {
		d.stripHolder.Objects = nil
		d.collapsedBar.Objects = []fyne.CanvasObject{newDivider(false), d.strip}
	}
	d.collapsedBar.Refresh()
	d.stripHolder.Refresh()
}

// sync follows UIState.Dock: opens new tabs, closes removed ones, shows the tabs of the shown
// cluster and of no cluster in the strip (D-69), and the active one. A hidden cluster's tabs keep
// their streams.
func (d *dock) sync(st engine.UIState) {
	tabs := st.Dock
	seen := make(map[string]bool, len(tabs))
	for _, t := range tabs {
		seen[t.ID] = true
		c, ok := d.contents[t.ID]
		if !ok {
			d.contents[t.ID] = d.openTab(t)
		} else if c.logs != nil {
			c.logs.update(t)
		} else if isTerminal(t.Type) {
			if c.kubectl != nil {
				c.kubectl.update(t)
			}
			d.followSession(c, t) // a new session after dock.reconnect, its row (dockreconnect.go)
		} else if u, ok := c.editor.(interface{ update(engine.DockTab) }); ok { // a Helm Install tab's version
			u.update(t)
		}
	}
	for id, c := range d.contents {
		if !seen[id] {
			c.close()
			delete(d.contents, id)
		}
	}
	d.tabs = st.DockInScope()
	d.active = st.ActiveDock
	if !slices.ContainsFunc(d.tabs, func(t engine.DockTab) bool { return t.ID == d.active }) && len(d.tabs) > 0 {
		d.active = d.tabs[len(d.tabs)-1].ID
	}
	d.rebuildStrip()
	d.showActive()
}

// openTab starts opening a tab's stream off the main goroutine; until it is open, or if it fails,
// the tab says so.
func (d *dock) openTab(t engine.DockTab) *dockContent {
	c := &dockContent{tab: t, obj: container.NewStack(dockMessage("Opening "+t.Title+"…", colorMuted))}
	ctx, cancel := context.WithCancel(d.sh.ctx)
	c.cancel = cancel
	id := t.ID
	switch t.Type {
	case engine.DockLogs:
		c.logs = newLogTab(ctx, d.sh, c, t)
		c.logs.open()
	case engine.DockTerminal, engine.DockShell, engine.DockAttach, engine.DockNodeShell:
		if t.Type == engine.DockTerminal {
			c.kubectl = newKubectlOffer(d.sh, t)
		}
		d.startTerminal(ctx, c, id) // dockreconnect.go
	case engine.DockEdit:
		e := newEditorTab(d.sh, t)
		c.editor = e
		c.set(e.root)
	case engine.DockCreate:
		e := newCreateTab(d.sh, t)
		c.editor = e
		c.set(e.root)
	case engine.DockHelmUpgrade:
		e := newHelmUpgradeTab(d.sh, t)
		c.editor = e
		c.set(e.root)
	case engine.DockHelmInstall:
		e := newHelmInstallTab(d.sh, t)
		c.editor = e
		c.set(e.root)
	case engine.DockHelmRevisions: // a release's revisions (helmrevisions.go)
		e := newHelmRevisionsTab(d.sh, t)
		c.editor = e
		c.set(e.root)
	case engine.DockAudit: // the Audit Log (auditlog.go)
		e := newAuditLogTab(d.sh, t)
		c.editor = e
		c.set(e.root)
	case engine.DockDiagnostics: // the Diagnostics (diagnostics.go)
		e := newDiagnosticsTab(d.sh)
		c.editor = e
		c.set(e.root)
	case engine.DockJob: // a background job's progress (jobtab.go)
		e := newJobTab(d.sh, t)
		c.editor = e
		c.set(e.root)
	default:
		c.set(dockMessage(fmt.Sprintf("Cogpit cannot show %q tabs yet.", t.Type), colorMuted))
	}
	return c
}

// editorEvent hands an EventEditor to the editor tab it names.
func (d *dock) editorEvent(id string, rev uint64) {
	if c, ok := d.contents[id]; ok && c.editor != nil && !c.closed {
		c.editor.remote(rev)
	}
}

func (c *dockContent) set(o fyne.CanvasObject) {
	c.obj.Objects = []fyne.CanvasObject{o}
	c.obj.Refresh()
}

// close stops opening and closes the stream; closing may wait for a process, so it runs off the
// main goroutine.
func (c *dockContent) close() {
	if c.closed {
		return
	}
	c.closed = true
	c.cancel()
	if c.editor != nil {
		c.editor.close()
	}
	if cl := c.closer; cl != nil {
		go func() { _ = cl.Close() }() // the tab is gone; nobody to tell
	}
}

// closeAll closes every tab's stream when the window closes. A terminal stops drawing first, on
// the main goroutine, while Fyne still queues its fyne.Do calls (termview.Terminal.Stop, D-103);
// the session's close follows on a goroutine of its own, as for any closed tab.
func (d *dock) closeAll() {
	for id, c := range d.contents {
		if s, ok := c.closer.(interface{ Stop() }); ok {
			s.Stop()
		}
		c.close()
		delete(d.contents, id)
	}
}

func (d *dock) rebuildStrip() {
	objs := make([]fyne.CanvasObject, 0, len(d.tabs))
	d.agentReadMarks.buttons = map[string]*tabButton{}
	for _, t := range d.tabs {
		id := t.ID
		icon := theme.ComputerIcon()
		switch t.Type {
		case engine.DockLogs:
			icon = theme.DocumentIcon()
		case engine.DockEdit:
			icon = theme.DocumentCreateIcon()
		case engine.DockCreate:
			icon = theme.ContentAddIcon()
		case engine.DockAttach:
			icon = theme.MailAttachmentIcon()
		case engine.DockHelmUpgrade:
			icon = theme.Icon(commandIcons[upgradeTabCommand])
		case engine.DockHelmInstall:
			icon = theme.Icon(commandIcons[installTabCommand])
		case engine.DockHelmRevisions:
			icon = theme.Icon(commandIcons[revisionsCommand])
		case engine.DockAudit:
			icon = theme.ListIcon()
		case engine.DockDiagnostics:
			icon = theme.InfoIcon()
		case engine.DockJob:
			icon = theme.ViewRefreshIcon()
		}
		b := newTabButton(t.Title, icon, id == d.active && d.open(),
			func() { // another tab activated opens the dock in the engine; the shown one opens it here
				d.requestFocus(id) // the clicked tab takes the keys, the shown one too (dockfocus.go)
				d.keysLast = true  // Move Tab's keys are the dock's (tabmovekeys.go)
				if id != d.active {
					d.sh.dispatch("dock.activate", dockParams{ID: id}, nil)
				} else if !d.open() {
					d.setOpen(new(true))
				}
			},
			func() { d.sh.dispatch("dock.close", dockParams{ID: id}, nil) })
		d.agentReadMarks.mark(b, t) // agents may read it: the marker, flashing at a read (dockagentread.go)
		b.id, b.drag = id, d.drag
		b.onMenu = func(at fyne.Position) { d.tabMenu(id, at) } // tabmenu.go
		objs = append(objs, b)
	}
	d.tabRow.Objects = objs
	d.drag.follow() // a tab carried or its move pending (stripdrag.go)
	if len(objs) == 0 {
		d.drag.cancel()
	}
	d.tabRow.Refresh()
	d.updateToggle()
	d.updateAgentRead()
}

func (d *dock) showActive() {
	if c, ok := d.contents[d.active]; ok {
		d.body.Objects = []fyne.CanvasObject{c.obj}
	} else {
		d.body.Objects = nil
	}
	d.body.Refresh()
}

func (d *dock) updateToggle() {
	if len(d.tabs) == 0 {
		d.toggle.Hide()
		return
	}
	d.toggle.Show()
	if d.open() {
		d.toggle.SetIcon(theme.MenuDropDownIcon())
	} else {
		d.toggle.SetIcon(theme.MenuDropUpIcon())
	}
}

// dockMoveParams mirror dock.move's parameters.
type dockMoveParams struct {
	ID    string `json:"id"`
	Index int    `json:"index"`
}

// dockMessage is a centred line of text in a dock tab: opening, failed, unsupported.
func dockMessage(text string, c fyne.ThemeColorName) fyne.CanvasObject {
	return container.NewCenter(newTextLine(text, c, theme.SizeNameText, fyne.TextStyle{}))
}

// tabButton is one dock tab: an icon, the title and a close button; the active tab is raised and
// underlined in the accent colour.
type tabButton struct {
	widget.BaseWidget
	id     string     // the dock tab's id
	drag   *stripDrag // the dock's: the button drags its tab to another place (stripdrag.go)
	guard  dragGuard  // a drag pressed outside the button is not its own (VW20, dragguard.go)
	title  string
	icon   fyne.Resource
	active bool
	onTap  func()
	onMenu func(at fyne.Position) // the right-click menu (tabmenu.go)

	bg    *canvas.Rectangle
	under *canvas.Rectangle
	img   *iconImage
	txt   *textLine
	close *widget.Button
	// agentRead shows the marker of a terminal tab agents may read, after the title; flash
	// tints the tab while an agent's read shows (dockagentread.go).
	agentRead, flash bool
	mark             *iconImage
}

func newTabButton(title string, icon fyne.Resource, active bool, onTap, onClose func()) *tabButton {
	b := &tabButton{title: title, icon: icon, active: active, onTap: onTap,
		bg: canvas.NewRectangle(color.Transparent), under: canvas.NewRectangle(color.Transparent),
		img: newIconImage(), mark: newIconImage()}
	b.txt = newTextLine(title, theme.ColorNameForeground, theme.SizeNameText, fyne.TextStyle{})
	b.txt.truncate = true
	b.close = widget.NewButtonWithIcon("", theme.CancelIcon(), onClose)
	b.close.Importance = widget.LowImportance
	b.ExtendBaseWidget(b)
	return b
}

// Tapped implements fyne.Tappable.
func (b *tabButton) Tapped(*fyne.PointEvent) {
	if b.onTap != nil {
		b.onTap()
	}
}

var _ fyne.Draggable = (*tabButton)(nil)

// Dragged implements fyne.Draggable: the tab is carried to another place in the strip (GW4).
func (b *tabButton) Dragged(ev *fyne.DragEvent) {
	if b.drag != nil && b.guard.pass(b, ev) {
		b.drag.dragged(b, ev)
	}
}

// DragEnd implements fyne.Draggable.
func (b *tabButton) DragEnd() {
	if b.guard.end() && b.drag != nil {
		b.drag.ended(b)
	}
}

// carried reports whether the button's tab is carried: it is drawn raised.
func (b *tabButton) carried() bool { return b.drag != nil && b.drag.carries(b.id) }

// CreateRenderer implements fyne.Widget.
func (b *tabButton) CreateRenderer() fyne.WidgetRenderer {
	r := &tabButtonRenderer{b: b}
	r.Refresh()
	return r
}

type tabButtonRenderer struct{ b *tabButton }

func (r *tabButtonRenderer) dims() (pad, icon, maxText float32) {
	th := r.b.Theme()
	return th.Size(theme.SizeNameInnerPadding), th.Size(theme.SizeNameInlineIcon), th.Size(sizeDockTabMaxLen)
}

func (r *tabButtonRenderer) textWidth() float32 {
	_, _, maxText := r.dims()
	th := r.b.Theme()
	w := fyne.MeasureText(r.b.title, th.Size(theme.SizeNameText), fyne.TextStyle{}).Width
	return fyne.Min(w, maxText)
}

// markWidth is the room the agent-read marker takes after the title: its icon and a padding, or
// nothing without it.
func (r *tabButtonRenderer) markWidth() float32 {
	if !r.b.agentRead {
		return 0
	}
	pad, icon, _ := r.dims()
	return pad + icon
}

func (r *tabButtonRenderer) MinSize() fyne.Size {
	pad, icon, _ := r.dims()
	c := r.b.close.MinSize()
	return fyne.NewSize(pad+icon+pad+r.textWidth()+r.markWidth()+c.Width, fyne.Max(c.Height, icon+2*pad))
}

func (r *tabButtonRenderer) Layout(size fyne.Size) {
	b := r.b
	pad, icon, _ := r.dims()
	line := b.Theme().Size(theme.SizeNameSeparatorThickness) * 2
	b.bg.Resize(size)
	b.under.Move(fyne.NewPos(0, size.Height-line))
	b.under.Resize(fyne.NewSize(size.Width, line))
	b.img.img.Move(fyne.NewPos(pad, (size.Height-icon)/2))
	b.img.img.Resize(fyne.NewSize(icon, icon))
	c := b.close.MinSize()
	mark := r.markWidth()
	tw := size.Width - (pad + icon + pad) - mark - c.Width
	b.txt.Move(fyne.NewPos(pad+icon+pad, 0))
	b.txt.Resize(fyne.NewSize(fyne.Max(tw, 0), size.Height))
	b.mark.img.Move(fyne.NewPos(pad+icon+pad+fyne.Max(tw, 0)+pad, (size.Height-icon)/2))
	b.mark.img.Resize(fyne.NewSize(icon, icon))
	b.close.Move(fyne.NewPos(size.Width-c.Width, (size.Height-c.Height)/2))
	b.close.Resize(c)
}

func (r *tabButtonRenderer) Refresh() {
	b := r.b
	th := b.Theme()
	if b.active || b.carried() { // a carried tab raised as the active one (stripdrag.go)
		b.bg.FillColor = themeColor(th, colorDockTab)
		b.txt.SetColor(colorDockTabText)
		b.under.FillColor = themeColor(th, colorAccent)
		b.img.set(th, b.icon, colorAccent)
	} else {
		b.bg.FillColor = color.Transparent
		b.txt.SetColor(colorDockTabDim)
		b.under.FillColor = color.Transparent
		b.img.set(th, b.icon, colorMuted)
	}
	b.bg.Shadow = liftShadow(th, b.carried()) // carried (stripdrag.go)
	refreshAgentReadMark(th, b)               // dockagentread.go
	r.Layout(b.Size())
	b.bg.Refresh()
	b.under.Refresh()
}

func (r *tabButtonRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.b.bg, r.b.under, r.b.img.img, r.b.txt, r.b.mark.img, r.b.close}
}

func (r *tabButtonRenderer) Destroy() {}
