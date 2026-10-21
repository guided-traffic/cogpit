package fyneui

import (
	"image/color"
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/kubeconfig"
)

// The navigator's section level (ui-parity › Layout and Clusters, G5, G13): the level above a
// cluster's tree. Lens's documentation lists the navigator's sections, Kubernetes Clusters (Local
// Kubeconfigs and the cloud sections), Teamwork and Extensions; its captures show the scope
// dropdown naming a section such as "Local Kubeconfigs", a cluster's root with a › and a ‹ to go
// back. The scope is engine state (UIState.NavigatorScope, engine/navscope.go): this level shows
// while it is the section, the tree (navigator.go) while it is a cluster.
//
// The section is the one list that chooses a cluster (D-57, the owner's interview of 2026-10-15
// evening): ⌘P, the Cluster menu's and the palette's Connect to Cluster, the welcome page's button
// and a --context that fails show it with its search focused (findcluster.go); the context picker,
// the top bar's "Switch context" and the header's scope dropdown went. The header reads "All
// Clusters" here, with a "+" that adds kubeconfigs (navigator_header.go).
//
// Cogpit's defaults (inferred, not the owner's decision, 2026-10-13):
//   - one section, "Local Kubeconfigs" under "Kubernetes Clusters", as the level's root; the cloud
//     sections (after the beta, G18), Teamwork (D-9) and Extensions (D-5) are not built, so the
//     root has no ‹ and nothing above it;
//   - every context the kubeconfig sources offer, grouped under a heading naming the folder or
//     file they come from (".kube/", "dev.yaml"), the demo cluster first without one; each row the
//     cluster's badge (its identity), its display name, a connection dot (the cluster's mark,
//     clustermarks.go: each connected cluster's state, D-69; red after a failed connect; grey
//     otherwise) and a ›; the shown cluster's row marked;
//   - the row's tooltip, "<name> - <source>", and below it what the context picker's row said
//     beside the context (the lead's night decision, 2026-10-15, for the owner to confirm; D-57):
//     why the connect failed (the connection's error for the cluster that just failed, else that
//     its log says why), a login program or an unchecked connection waiting for the user's
//     approval, and a certificate the kubeconfig does not check, with the server the credential
//     goes to; a row whose last connect failed has Show Connection Updates… in its menu, the log
//     the picker's warning icon opened (G2);
//   - under the name of a context whose user logs in with a client certificate, its time left or
//     since when it expired (D-75, the owner's F22 of 2026-10-17; clientcert.go);
//   - a click on a row or its ›, → or ↵ drills into the cluster (ui.setNavigatorScope): a cluster
//     not connected is connected through the engine, its login program shown for approval first;
//     ↑↓, Home and End move, ← goes to the cluster's heading (or the root), Escape clears the
//     search (on ⌘P's empty search goes back to the cluster it came from, navHeader.escape) or
//     closes the drawer, a letter starts the search, F1–F12 reach the hotbar;
//   - a source group's heading collapses and opens its group (the owner's A63, 2026-10-14 evening):
//     a click, ↵ or Space toggle it, ← collapses an open one (else goes to the root), → opens a
//     collapsed one (else moves into it); a collapsed heading says how many clusters it hides. Which
//     groups are collapsed is engine state (ui.setNavigatorGroup, NavigatorState.Collapsed), kept
//     across restarts in workspace.json (D-41); while a search is on every group shows open, as the
//     tree's groups do (Cogpit's default);
//   - the keyboard's row has the accent ring while the list has the focus from the keyboard, not
//     after a click (A53), and is engine state (A51, navigatorstate.go);
//   - the header's search filters the clusters by name, context and source, ↵ drills into the
//     first match;
//   - a row's right-click menu is the cluster's (Connect, Settings…, the hotbar's item,
//     Disconnect, Remove from Cogpit, Show Connection Updates…), the root's has Add Kubeconfigs…, as
//     the header's "+" does.

// The section's names (Lens's, from its documentation and captures).
const (
	navSectionTitle  = "Local Kubeconfigs"
	navSectionParent = "Kubernetes Clusters"
	// navSectionRoot is the keyboard's id of the section's root row; context ids never take it.
	navSectionRoot = "\x00section"
	// navSourcePrefix starts the keyboard's id of a source group's heading, followed by its label.
	navSourcePrefix = "\x00source:"
)

// isSourceHead reports whether a keyboard id is a source group's heading.
func isSourceHead(id string) bool { return strings.HasPrefix(id, navSourcePrefix) }

// navScopeParams are ui.setNavigatorScope's parameters, mirroring the engine's schema.
type navScopeParams struct {
	Scope string `json:"scope"`
}

// navSections is the navigator's section level: its root and the clusters below it.
type navSections struct {
	widget.BaseWidget
	s *sidebar

	root    *navSectionRootRow
	list    *fyne.Container
	scroll  *container.Scroll
	hint    *textLine
	hintRow fyne.CanvasObject

	contexts []engine.ContextInfo // as the kubeconfig sources offer them, read off the main goroutine
	loaded   bool
	loadSeq  int
	loadErr  string

	rows    map[string]*navContextRow // the rows made, by context id
	heads   map[string]*navSourceRow  // the groups' headings made, by group
	visible []string                  // the keyboard's rows in order: navSectionRoot, then the headings and the shown contexts

	// collapsed are the source groups shown collapsed: the engine's (NavigatorState.Collapsed),
	// changed here at once on a click while its ui.setNavigatorGroup is under way (groupBusy).
	collapsed map[string]bool
	groupBusy int

	focus   string // the row the keyboard is on
	focused bool   // the list has the window's keyboard focus
	keys    bool   // the keyboard moved last, not the pointer: the ring shows (A53)
	pointer bool   // the focus about to come is a click's
	shift   bool   // Shift is held (a shifted F-key is not the hotbar's, D-83)

	menuModel *fyne.Menu // a row's open menu, for tests
	subOnce   sync.Once

	// mixed is the foot's "Mixed environments" check (the owner, 2026-10-20, GW11): the preference
	// mixedEnvironments, every cluster's tabs in the tab bar; off, the default, only the shown
	// cluster's (engine/navscope.go TabsShown). syncing is set while the UI state sets it.
	mixed   *widget.Check
	foot    fyne.CanvasObject
	syncing bool
}

// mixedEnvironmentsHint is the "?" tooltip after the check.
const mixedEnvironmentsHint = "On: the tab bar shows the open tabs of every connected cluster side by side.\n" +
	"Off: only the tabs of the cluster you are in, the one the status bar and the navigator name; another cluster's tabs show when you go to that cluster."

var (
	_ fyne.Focusable  = (*navSections)(nil)
	_ desktop.Keyable = (*navSections)(nil)
)

func newNavSections(s *sidebar) *navSections {
	n := &navSections{s: s, rows: map[string]*navContextRow{}, heads: map[string]*navSourceRow{}, visible: []string{navSectionRoot},
		collapsed: map[string]bool{}}
	n.root = newNavSectionRootRow(n)
	n.hint = newTextLine("", colorMuted, theme.SizeNameCaptionText, fyne.TextStyle{})
	n.hint.truncate = true
	n.hintRow = inset(theme.SizeNameInnerPadding, theme.SizeNameInnerPadding, n.hint)
	n.list = container.New(layout.NewVBoxLayout(), n.root)
	n.scroll = container.NewVScroll(inset(theme.SizeNamePadding, theme.SizeNamePadding, n.list))
	n.mixed = widget.NewCheck("Mixed environments", func(on bool) {
		if !n.syncing {
			n.s.sh.dispatch("preferences.set", map[string]bool{"mixedEnvironments": on}, nil)
		}
	})
	n.foot = container.NewVBox(newDivider(false), inset(theme.SizeNamePadding, theme.SizeNamePadding,
		container.NewHBox(n.mixed, newFieldName("", mixedEnvironmentsHint, func() *tooltip { return s.sh.tip }))))
	n.ExtendBaseWidget(n)
	return n
}

// CreateRenderer implements fyne.Widget: the clusters, and at the foot the mixed environments check.
func (n *navSections) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(container.NewBorder(nil, n.foot, nil, nil, n.scroll))
}

// syncMixed sets the foot's check from the UI state's preferences.
func (n *navSections) syncMixed() {
	on := mixedEnvironments(n.s.sh.ui)
	if n.mixed.Checked == on {
		return
	}
	n.syncing = true
	n.mixed.SetChecked(on)
	n.syncing = false
}

// load reads the contexts off the main goroutine, then shows them; a newer load supersedes an
// older one. It also starts following the contexts (a kubeconfig synced or removed).
func (n *navSections) load() {
	n.subscribe()
	n.loadSeq++
	seq := n.loadSeq
	sh := n.s.sh
	go func() {
		defer sh.recoverTo("the navigator's clusters")
		ctxs, err := sh.eng.Contexts() // a partial list is shown with what failed
		sh.do(func() {
			if seq != n.loadSeq {
				return
			}
			n.contexts, n.loaded, n.loadErr = ctxs, true, ""
			if err != nil {
				n.loadErr = err.Error()
			}
			n.render()
			if sh.hot != nil {
				sh.hot.marksChanged() // the tiles' marks read the contexts' failed connects too (connectProblem)
			}
		})
	}()
}

// subscribe reloads the contexts when the engine reports that they changed, for as long as the
// shell runs. It never touches a widget on the engine's goroutine.
func (n *navSections) subscribe() {
	n.subOnce.Do(func() {
		sh := n.s.sh
		cancel := sh.eng.Subscribe(func(ev engine.Event) {
			if ev.Type == engine.EventContexts {
				sh.do(n.load)
			}
		})
		go func() {
			<-sh.ctx.Done()
			cancel()
		}()
	})
}

// context returns a listed context.
func (n *navSections) context(id string) (engine.ContextInfo, bool) {
	i := slices.IndexFunc(n.contexts, func(c engine.ContextInfo) bool { return c.Name == id })
	if i < 0 {
		return engine.ContextInfo{}, false
	}
	return n.contexts[i], true
}

// sourceGroup is the group a context is listed under: the synced folder its file is in (".kube/"),
// or the file the user added ("dev.yaml"); "" for a context without a kubeconfig source (the demo
// cluster).
func sourceGroup(c engine.ContextInfo) string {
	if i := strings.LastIndex(c.Source, "/"); i >= 0 {
		return c.Source[:i+1]
	}
	return c.Source
}

// rowTitle is a context's name in the section: its display name (cluster settings), else the
// context's name without its file; the demo cluster's is short.
func (n *navSections) rowTitle(c engine.ContextInfo) string {
	if c.Demo && n.s.sh.ui.ClusterSettings[c.Name].Name == "" {
		return "Demo cluster"
	}
	return kubeconfig.DisplayText(n.s.sh.identity(c.Name).name, maxShownPath)
}

// matches reports whether a context matches the search, ignoring case: its name, display name or
// source.
func (n *navSections) matches(c engine.ContextInfo, f string) bool {
	if f == "" {
		return true
	}
	return strings.Contains(strings.ToLower(c.Name+" "+n.rowTitle(c)+" "+c.Source), strings.ToLower(f))
}

// groups are the contexts matching filter by group, in order: the contexts without a source first,
// then the groups by name; within a group as the engine lists them (by id).
func (n *navSections) groups(filter string) (keys []string, byGroup map[string][]engine.ContextInfo) {
	byGroup = map[string][]engine.ContextInfo{}
	for _, c := range n.contexts {
		if !n.matches(c, filter) {
			continue
		}
		g := sourceGroup(c)
		if _, ok := byGroup[g]; !ok {
			keys = append(keys, g)
		}
		byGroup[g] = append(byGroup[g], c)
	}
	slices.Sort(keys) // "" first
	return keys, byGroup
}

// listedAbove reports whether All Clusters lists context a above context b, searched or collapsed
// or not: the order a change of cluster slides in (navigator_slide.go). A context not listed is
// below every other.
func (n *navSections) listedAbove(a, b string) bool {
	keys, byGroup := n.groups("")
	ia, ib := -1, -1
	i := 0
	for _, g := range keys {
		for _, c := range byGroup[g] {
			switch c.Name {
			case a:
				ia = i
			case b:
				ib = i
			}
			i++
		}
	}
	return ia >= 0 && (ib < 0 || ia < ib)
}

// groupOpen reports whether a source group shows its clusters: the user has not collapsed it, or a
// search is on.
func (n *navSections) groupOpen(g string) bool { return n.s.filter != "" || !n.collapsed[g] }

// render lays the rows out again: after a load, a search, a connection, an identity or the
// collapsed groups changed.
func (n *navSections) render() {
	keys, byGroup := n.groups(n.s.filter)
	objs := []fyne.CanvasObject{n.root}
	visible := []string{navSectionRoot}
	rows := make(map[string]*navContextRow, len(n.rows))
	heads := make(map[string]*navSourceRow, len(n.heads))
	count := 0
	for _, g := range keys {
		if g != "" {
			h := n.heads[g]
			if h == nil {
				h = newNavSourceRow(n, g)
			}
			h.set(len(byGroup[g]), n.groupOpen(g))
			heads[g] = h
			objs = append(objs, h)
			visible = append(visible, navSourcePrefix+g)
			if !n.groupOpen(g) {
				count += len(byGroup[g])
				continue
			}
		}
		for _, c := range byGroup[g] {
			r := n.rows[c.Name]
			if r == nil {
				r = newNavContextRow(n, c.Name)
			}
			r.set(c)
			rows[c.Name] = r
			objs = append(objs, r)
			visible = append(visible, c.Name)
			count++
		}
	}
	if hint := n.hintText(count); hint != "" {
		n.hint.SetText(hint)
		objs = append(objs, n.hintRow)
	}
	if tip := n.s.sh.tip; tip != nil {
		if owner, ok := tip.owner.(*navContextRow); ok && rows[owner.id] != owner {
			tip.hide() // its row is gone and would never say the pointer left
		}
		if owner, ok := tip.owner.(*navSourceRow); ok && heads[owner.group] != owner {
			tip.hide()
		}
	}
	n.rows, n.heads, n.visible = rows, heads, visible
	if n.loaded && !slices.Contains(visible, n.focus) { // before the first read, the cluster it is to go to
		n.focus = n.shownAbove(n.focus)
	}
	n.root.update(len(n.contexts))
	n.list.Objects = objs
	n.scroll.Refresh()
	n.s.noteRow() // a row the search or a collapsed group hid moved the keyboard (navigatorstate.go)
}

// shownAbove is the shown row above a hidden one: a cluster's heading when it shows, else the root.
func (n *navSections) shownAbove(id string) string {
	if c, ok := n.context(id); ok && sourceGroup(c) != "" && slices.Contains(n.visible, navSourcePrefix+sourceGroup(c)) {
		return navSourcePrefix + sourceGroup(c)
	}
	return navSectionRoot
}

// refreshCerts shows the client certificates' lines as the clock moved on (shell.certsTick): only
// the rows whose line changed are laid out again.
func (n *navSections) refreshCerts() {
	changed := false
	for _, r := range n.rows {
		if r.info.ClientCert != nil && r.setCert() {
			changed = true
		}
	}
	if changed {
		n.list.Refresh()
	}
}

// isShown reports whether a keyboard id is a row the section shows.
func (n *navSections) isShown(id string) bool { return slices.Contains(n.visible, id) }

// headOf is the keyboard id of a shown cluster's heading, "" for one without (the demo cluster).
func (n *navSections) headOf(id string) string {
	if c, ok := n.context(id); ok && sourceGroup(c) != "" && n.isShown(navSourcePrefix+sourceGroup(c)) {
		return navSourcePrefix + sourceGroup(c)
	}
	return ""
}

// setCollapsed collapses or opens a source group: at once in the window, and in the engine through
// ui.setNavigatorGroup, which keeps it across restarts; the engine's answer is shown once every
// change sent is done.
func (n *navSections) setCollapsed(g string, collapsed bool) {
	if g == "" || n.collapsed[g] == collapsed {
		return
	}
	if collapsed {
		n.collapsed[g] = true
	} else {
		delete(n.collapsed, g)
	}
	n.render()
	n.groupBusy++
	n.s.sh.dispatch("ui.setNavigatorGroup", navGroupParams{Group: g, Collapsed: collapsed}, func(_ any, err error) {
		n.groupBusy--
		n.s.sh.showError(err)
		n.collapsedChanged()
	})
}

// toggle collapses an open group or opens a collapsed one (not while a search shows them all).
func (n *navSections) toggle(g string) {
	if n.s.filter == "" {
		n.setCollapsed(g, !n.collapsed[g])
	}
}

// collapsedChanged shows the engine's collapsed groups, unless a change of the window's is under way.
func (n *navSections) collapsedChanged() {
	if n.groupBusy > 0 {
		return
	}
	want := map[string]bool{}
	for _, g := range n.s.sh.ui.Navigator.Collapsed {
		want[g] = true
	}
	if maps.Equal(want, n.collapsed) {
		return
	}
	n.collapsed = want
	n.render()
}

// hintText is the line below the clusters when there is something to say.
func (n *navSections) hintText(shown int) string {
	switch {
	case !n.loaded:
		return "Reading the kubeconfigs…"
	case shown > 0 && n.loadErr != "":
		return "Some kubeconfigs could not be read: " + n.loadErr
	case shown > 0:
		return ""
	case n.loadErr != "":
		return "Could not read the kubeconfigs: " + n.loadErr
	case n.s.filter != "":
		return "No clusters match “" + n.s.filter + "”."
	}
	return "No clusters yet: add a kubeconfig with the + above."
}

// marked reports whether a context is the active cluster: connected or connecting.
func (n *navSections) marked(id string) bool {
	ui := n.s.sh.ui
	return id == ui.Context && (ui.Connection.State.Live() || ui.Connection.State == engine.Connecting)
}

// dotMark is what a context's connection dot shows (clustermarks.go, D-69): the shown cluster's
// connection, a connected one's mark (warm, cold, its login lapsed, reconnecting), a failed connect
// after one failed, not connected otherwise.
func (n *navSections) dotMark(c engine.ContextInfo) clusterMark {
	return n.s.sh.mark(c.Name, c.ConnectProblem)
}

// drill shows a cluster's tree (ui.setNavigatorScope). A connected cluster is only shown (D-69);
// a cluster not connected is connected through the engine: a login program the user has not
// allowed yet, or that changed, is shown for approval first (kubeconfigs.go), and nothing runs
// without it.
func (n *navSections) drill(id string) {
	sh := n.s.sh
	if !n.marked(id) && !switchable(sh.ui, id) {
		if c, ok := n.context(id); ok && (c.Exec == engine.ExecRequired || c.Exec == engine.ExecChanged) {
			sh.kc.reviewPlugin(id, func(allowed bool) {
				if allowed {
					n.dispatchScope(id)
				}
			})
			return
		}
	}
	n.dispatchScope(id)
}

func (n *navSections) dispatchScope(id string) {
	n.s.setScope(engine.NavScopeCluster(id))
}

// openFirstMatch drills into the first cluster the search shows (↵ in the search field).
func (n *navSections) openFirstMatch() {
	for _, id := range n.visible[1:] {
		if !isSourceHead(id) {
			n.setFocus(id)
			n.drill(id)
			return
		}
	}
}

// shownFrom is called when the section level shows: the keyboard goes to the cluster the navigator
// came from, and the contexts are read again (unless the first read is still under way).
func (n *navSections) shownFrom(cluster string) {
	if cluster != "" {
		n.focus = cluster
	}
	if n.loaded {
		n.load()
	}
	n.render()
}

// rowOf returns the row object of a shown id.
func (n *navSections) rowOf(id string) fyne.CanvasObject {
	if id == navSectionRoot {
		return n.root
	}
	if g, ok := strings.CutPrefix(id, navSourcePrefix); ok {
		if h := n.heads[g]; h != nil {
			return h
		}
		return nil
	}
	if r := n.rows[id]; r != nil {
		return r
	}
	return nil
}

func (n *navSections) refreshRow(id string) {
	if r := n.rowOf(id); r != nil {
		r.Refresh()
	}
}

// setFocus moves the keyboard to a shown row and scrolls it into view.
func (n *navSections) setFocus(id string) {
	if id != n.focus {
		old := n.focus
		n.focus = id
		n.refreshRow(old)
		n.refreshRow(id)
	}
	defer n.s.noteRow()
	row := n.rowOf(id)
	if row == nil || !row.Visible() {
		return
	}
	y, h, view, off := n.list.Position().Y+row.Position().Y, row.Size().Height, n.scroll.Size().Height, n.scroll.Offset.Y
	switch {
	case view <= 0:
	case y < off:
		n.scroll.ScrollToOffset(fyne.NewPos(0, y))
	case y+h > off+view:
		n.scroll.ScrollToOffset(fyne.NewPos(0, y+h-view))
	}
}

// keyed marks the keyboard as moving: the ring shows on the keyboard's row (A53).
func (n *navSections) keyed() {
	if !n.keys {
		n.keys = true
		n.refreshRow(n.focus)
	}
}

// step moves the keyboard d rows down (up for a negative d), stopping at the ends.
func (n *navSections) step(d int) {
	i := slices.Index(n.visible, n.focus)
	switch {
	case i < 0:
		i = 0
	case i+d >= 0 && i+d < len(n.visible):
		i += d
	}
	n.setFocus(n.visible[i])
}

// tapped is a click on a row: the list takes the keyboard there without the ring (A53); a cluster's
// tree shows, a heading collapses or opens its group.
func (n *navSections) tapped(id string) {
	old := n.focus
	n.keys = false
	n.setFocus(id)
	n.refreshRow(old)
	if c := n.s.sh.win.Canvas(); c.Focused() != n {
		n.pointer = true
		c.Focus(n)
		n.pointer = false
	}
	switch g, head := strings.CutPrefix(id, navSourcePrefix); {
	case head:
		n.toggle(g)
	case id != navSectionRoot:
		n.drill(id)
	}
}

// enter moves the keyboard into the list from the search field: to the first match.
func (n *navSections) enter() {
	if len(n.visible) > 1 {
		n.focus = n.visible[1]
	}
	n.keys = true
	n.s.sh.win.Canvas().Focus(n)
	n.setFocus(n.focus)
}

// TypedKey implements fyne.Focusable: the section's keyboard.
func (n *navSections) TypedKey(ev *fyne.KeyEvent) {
	if n.s.sh.hot.key(ev.Name, n.shift) { // F1–F12 belong to the hotbar
		return
	}
	n.keyed()
	g, head := strings.CutPrefix(n.focus, navSourcePrefix)
	switch ev.Name {
	case fyne.KeyUp:
		n.step(-1)
	case fyne.KeyDown:
		n.step(1)
	case fyne.KeyHome:
		n.setFocus(n.visible[0])
	case fyne.KeyEnd:
		n.setFocus(n.visible[len(n.visible)-1])
	case fyne.KeyRight:
		switch {
		case head && !n.groupOpen(g):
			n.setCollapsed(g, false)
		case head || n.focus == navSectionRoot:
			n.step(1) // into the group, or from the root to the first row, as the tree's → does
		default:
			n.drill(n.focus)
		}
	case fyne.KeyReturn, fyne.KeyEnter, fyne.KeySpace:
		switch {
		case head:
			n.toggle(g)
		case n.focus != navSectionRoot:
			n.drill(n.focus)
		}
	case fyne.KeyLeft:
		switch {
		case head && n.groupOpen(g) && n.s.filter == "":
			n.setCollapsed(g, true)
		case !head && n.headOf(n.focus) != "":
			n.setFocus(n.headOf(n.focus)) // a cluster's heading, as the tree's ← goes to the group
		case n.focus != navSectionRoot:
			n.setFocus(navSectionRoot) // nothing above the section: the root is as far up as it goes
		}
	case fyne.KeyEscape:
		if n.s.head.searching() {
			n.s.head.escape() // ⌘P's empty search goes back to its cluster (D-57)
			return
		}
		n.s.sh.closeDrawer()
	}
}

// TypedRune implements fyne.Focusable: a letter typed in the list starts a search with it.
func (n *navSections) TypedRune(r rune) {
	if unicode.IsPrint(r) && !unicode.IsSpace(r) {
		n.s.head.openSearch(n.s.filter + string(r))
	}
}

// KeyDown implements desktop.Keyable: it tracks Shift, which F-keys reach TypedKey without.
func (n *navSections) KeyDown(ev *fyne.KeyEvent) {
	if ev.Name == desktop.KeyShiftLeft || ev.Name == desktop.KeyShiftRight {
		n.shift = true
	}
}

// KeyUp implements desktop.Keyable.
func (n *navSections) KeyUp(ev *fyne.KeyEvent) {
	if ev.Name == desktop.KeyShiftLeft || ev.Name == desktop.KeyShiftRight {
		n.shift = false
	}
}

// FocusGained implements fyne.Focusable: the keyboard starts on the active cluster's row, else on
// the first row; the ring shows unless a click gave the focus (A53).
func (n *navSections) FocusGained() {
	n.focused, n.keys = true, !n.pointer
	if !slices.Contains(n.visible, n.focus) || n.focus == navSectionRoot {
		n.focus = navSectionRoot
		for _, id := range n.visible[1:] {
			if n.marked(id) {
				n.focus = id
			}
		}
		if n.focus == navSectionRoot && len(n.visible) > 1 {
			n.focus = n.visible[1]
		}
	}
	n.setFocus(n.focus)
	n.refreshRow(n.focus)
}

// FocusLost implements fyne.Focusable.
func (n *navSections) FocusLost() {
	n.focused, n.shift = false, false
	n.refreshRow(n.focus)
}

// keyboardOn reports whether the ring is on a row: the list has the focus from the keyboard (A53).
func (n *navSections) keyboardOn(id string) bool { return n.focused && n.keys && n.focus == id }

// rowMenu is a cluster's menu (right-click on its row): Connect while it is not the active one
// (Show for a connected one, D-69), Settings…, the hotbar's item, Disconnect, Remove from Cogpit,
// as its menus elsewhere have them, and after a failed connect Show Connection Updates…, its log
// (G2).
func (n *navSections) rowMenu(id string, pos fyne.Position) {
	sh := n.s.sh
	var items []*fyne.MenuItem
	if !n.marked(id) {
		items = append(items, fyne.NewMenuItem(connectItemLabel(sh.ui, id), func() { n.drill(id) }))
	}
	for _, item := range append([]*fyne.MenuItem{sh.settingsItem(id), sh.hot.contextItem(id)}, sh.clusterItems(id)...) {
		if item != nil {
			items = append(items, item)
		}
	}
	if c, ok := n.context(id); ok && c.ConnectProblem {
		items = append(items, fyne.NewMenuItemSeparator(), fyne.NewMenuItem(connectLogItem, func() { sh.kc.showConnectLog(id) }))
	}
	if len(items) == 0 {
		return
	}
	if items[0].IsSeparator {
		items = items[1:]
	}
	n.showMenu(items, pos)
}

func (n *navSections) showMenu(items []*fyne.MenuItem, pos fyne.Position) {
	sh := n.s.sh
	sh.tip.hide()
	n.menuModel = fyne.NewMenu("", items...)
	widget.NewPopUpMenu(n.menuModel, sh.win.Canvas()).ShowAtPosition(pos)
}

// navSectionRootRow is the section's root: "Local Kubeconfigs" with "Kubernetes Clusters" and the
// number of clusters below it, and a "−" at its right that opens Kubeconfig Sources…, where synced
// files and folders are removed (the owner's WF44 of 2026-10-19: the dialog was hard to find; the
// header's "+" adds them). Its menu (right-click) adds kubeconfigs and opens the sources.
type navSectionRootRow struct {
	widget.BaseWidget
	n        *navSections
	icon     *widget.Icon
	title    *textLine
	sub      *textLine
	sources  *navIconButton // "−": Kubeconfig Sources…
	body     fyne.CanvasObject
	bg, ring *canvas.Rectangle
	hovered  bool
}

var (
	_ fyne.Tappable          = (*navSectionRootRow)(nil)
	_ fyne.SecondaryTappable = (*navSectionRootRow)(nil)
	_ desktop.Hoverable      = (*navSectionRootRow)(nil)
)

func newNavSectionRootRow(n *navSections) *navSectionRootRow {
	r := &navSectionRootRow{n: n, bg: canvas.NewRectangle(color.Transparent), ring: canvas.NewRectangle(color.Transparent)}
	r.icon = widget.NewIcon(themedIcon(theme.FolderOpenIcon(), theme.ColorNameForeground))
	r.title = newTextLine(navSectionTitle, theme.ColorNameForeground, theme.SizeNameText, fyne.TextStyle{Bold: true})
	r.sub = newTextLine(navSectionParent, colorMuted, theme.SizeNameCaptionText, fyne.TextStyle{})
	r.title.truncate, r.sub.truncate = true, true
	side := sized(sizeNavBadge, sizeNavBadge, container.NewCenter(r.icon))                                          // the clusters' badges' column
	r.sources = newNavIconButton(n.s.sh, theme.ContentRemoveIcon(), sourcesTip, func() { n.s.sh.kc.showSources() }) // kc is made after the sidebar
	// The "−" on the title's line, so that the line under it keeps the row's width.
	titleLine := container.NewBorder(nil, nil, nil, r.sources, r.title)
	r.body = inset(theme.SizeNamePadding, theme.SizeNamePadding, container.NewBorder(nil, nil,
		container.NewCenter(side), nil, container.NewVBox(layout.NewSpacer(), titleLine, r.sub, layout.NewSpacer())))
	r.ExtendBaseWidget(r)
	return r
}

// update shows the number of clusters the section holds.
func (r *navSectionRootRow) update(clusters int) {
	text := navSectionParent
	if r.n.loaded {
		text += " · " + strconv.Itoa(clusters) + " cluster" + plural(clusters)
	}
	r.sub.SetText(text)
}

// Tapped implements fyne.Tappable: the keyboard goes to the root.
func (r *navSectionRootRow) Tapped(*fyne.PointEvent) {
	r.n.s.sh.tip.hideFor(r)
	r.n.tapped(navSectionRoot)
}

// TappedSecondary implements fyne.SecondaryTappable: Add Kubeconfigs… (the Sync Files and Folders
// dialog; Cogpit's, inferred) and Kubeconfig Sources… (WF44).
func (r *navSectionRootRow) TappedSecondary(ev *fyne.PointEvent) {
	sh := r.n.s.sh
	r.n.showMenu([]*fyne.MenuItem{fyne.NewMenuItem("Add Kubeconfigs…", sh.kc.addKubeconfigs),
		fyne.NewMenuItem(sourcesLabel, sh.kc.showSources)}, ev.AbsolutePosition)
}

// sourcesLabel opens the dialog of the kubeconfig sources; sourcesTip is the section row's "−"
// that opens it (WF44).
const (
	sourcesLabel = "Kubeconfig Sources…"
	sourcesTip   = "Kubeconfig Sources…: remove synced files and folders"
)

// MouseIn implements desktop.Hoverable.
func (r *navSectionRootRow) MouseIn(ev *desktop.MouseEvent) {
	r.hovered = true
	r.Refresh()
	r.n.s.sh.tip.hover(r, navSectionTitle+" - "+navSectionParent, ev.AbsolutePosition)
}

// MouseMoved implements desktop.Hoverable.
func (r *navSectionRootRow) MouseMoved(ev *desktop.MouseEvent) {
	r.n.s.sh.tip.moved(r, ev.AbsolutePosition)
}

// MouseOut implements desktop.Hoverable.
func (r *navSectionRootRow) MouseOut() {
	r.hovered = false
	r.Refresh()
	r.n.s.sh.tip.hideFor(r)
}

// CreateRenderer implements fyne.Widget.
func (r *navSectionRootRow) CreateRenderer() fyne.WidgetRenderer {
	rr := &rowBodyRenderer{w: r, body: r.body, bg: r.bg, ring: r.ring, paint: func() {
		paintRow(r.Theme(), r.bg, r.ring, false, r.hovered, r.n.keyboardOn(navSectionRoot))
	}}
	rr.Refresh()
	return rr
}

// navContextRow is one cluster of the section: its badge, display name, connection dot and ›,
// and a warning icon before the dot for a context whose kubeconfig does not check the server's
// certificate (the picker's pill until D-57; the tooltip says why). Under the name, for a context
// whose user logs in with a client certificate, how long it is valid (D-75, clientcert.go). A
// connected cluster has a ↻ before its dot: Reconnect (reconnect.go, D-81), dimmed while it runs.
type navContextRow struct {
	widget.BaseWidget
	n         *navSections
	id        string
	info      engine.ContextInfo
	badge     *clusterBadge
	title     *textLine
	cert      *textLine // the client certificate's time left or "expired" (D-75); hidden without one
	insecure  *widget.Icon
	reconnect *navIconButton // Reconnect, for a connected cluster; hidden otherwise
	dot       *connDot
	next      *navIconButton
	body      fyne.CanvasObject
	bg, ring  *canvas.Rectangle
	hovered   bool
}

var (
	_ fyne.Tappable          = (*navContextRow)(nil)
	_ fyne.SecondaryTappable = (*navContextRow)(nil)
	_ desktop.Hoverable      = (*navContextRow)(nil)
)

func newNavContextRow(n *navSections, id string) *navContextRow {
	r := &navContextRow{n: n, id: id, badge: newClusterBadge(), dot: newConnDot(n.s.sh),
		bg: canvas.NewRectangle(color.Transparent), ring: canvas.NewRectangle(color.Transparent)}
	r.title = newTextLine("", theme.ColorNameForeground, theme.SizeNameText, fyne.TextStyle{})
	r.title.truncate = true
	r.cert = newTextLine("", colorMuted, theme.SizeNameCaptionText, fyne.TextStyle{})
	r.cert.truncate = true
	r.cert.Hide()
	r.next = newNavIconButton(n.s.sh, iconChevronRight, "", func() { n.tapped(id) })
	r.reconnect = newNavIconButton(n.s.sh, reconnectIcon(), reconnectTip, func() { n.s.sh.reconnect(id) })
	r.reconnect.Hide()
	r.insecure = widget.NewIcon(themedIcon(theme.WarningIcon(), colorStatusWarning))
	r.insecure.Hide()
	r.body = inset(theme.SizeNamePadding, theme.SizeNamePadding, container.NewBorder(nil, nil,
		container.NewHBox(container.NewCenter(r.badge), newSpace(theme.SizeNamePadding)),
		container.NewHBox(container.NewCenter(r.insecure), container.NewCenter(r.reconnect), container.NewCenter(r.dot), container.NewCenter(r.next)),
		container.NewVBox(layout.NewSpacer(), r.title, r.cert, layout.NewSpacer())))
	r.ExtendBaseWidget(r)
	return r
}

// setCert shows the client certificate's line as of now (D-75): its time left or since when it
// expired, in its state's colour; hidden for a context without one. It reports whether the line
// changed.
func (r *navContextRow) setCert() bool {
	text, color := certLine(r.info.ClientCert, r.n.s.sh.now())
	if text == r.cert.text && color == r.cert.color && (text != "") == r.cert.Visible() {
		return false
	}
	r.cert.SetText(text)
	r.cert.SetColor(color)
	if text == "" {
		r.cert.Hide()
	} else {
		r.cert.Show()
	}
	return true
}

// set shows a context as listed now.
func (r *navContextRow) set(c engine.ContextInfo) {
	n := r.n
	r.info = c
	r.badge.setIdentity(n.s.sh.identity(c.Name))
	title := n.rowTitle(c)
	r.title.SetText(title)
	if bold := n.marked(c.Name); bold != r.title.style.Bold {
		r.title.style.Bold = bold
		r.title.Refresh()
	}
	r.dot.setMark(c.Name, n.dotMark(c))
	r.setReconnect()
	r.setCert()
	if c.Insecure != r.insecure.Visible() {
		if c.Insecure {
			r.insecure.Show()
		} else {
			r.insecure.Hide()
		}
	}
	r.next.tip = "Show " + title
	r.Refresh()
}

// setReconnect shows the ↻ for a connected cluster, muted and without its tap while it reconnects.
func (r *navContextRow) setReconnect() {
	ui := r.n.s.sh.ui
	if _, ok := r.n.s.sh.eng.Registry().Get("cluster.reconnect"); !ok || !reconnectable(ui, r.id) {
		r.reconnect.Hide()
		return
	}
	r.reconnect.Show()
	r.reconnect.muted = reconnecting(ui, r.id)
	r.reconnect.tip = reconnectTip
	if r.reconnect.muted {
		r.reconnect.tip = reconnectingText
	}
	r.reconnect.Refresh()
}

// connectLogItem is the row menu's item that opens the log of the last connect (connectlog.go).
const connectLogItem = "Show Connection Updates…"

// tipText is the row's tooltip: the context's full name (the row shows the short one, D-68) and the
// kubeconfig source it comes from, then a line for each thing to know before connecting (D-57): why
// the last connect failed, a login waiting for the user's approval, a certificate the kubeconfig
// does not check, and the client certificate's dates (D-75).
func (r *navContextRow) tipText() string {
	c, ui := r.info, r.n.s.sh.ui
	lines := []string{kubeconfig.DisplayText(engine.ContextLabel(c.Name), maxShownPath)}
	switch {
	case c.Source != "":
		lines[0] += " - " + kubeconfig.DisplayText(c.Source, maxShownPath)
	case c.Demo:
		lines[0] = demoLabel
	}
	switch {
	case c.Name == ui.Context && ui.Connection.State == engine.ConnectError && ui.Connection.Error != "":
		lines = append(lines, "Could not connect: "+kubeconfig.DisplayText(ui.Connection.Error, maxShownPath))
	case c.ConnectProblem:
		lines = append(lines, "The last connect failed: "+connectLogItem+" in its menu says why")
	}
	if state := loginState(c); state != "" {
		lines = append(lines, strings.ToUpper(state[:1])+state[1:]+": Cogpit shows it before it connects")
	}
	if c.Insecure {
		lines = append(lines, insecureNote(c))
	}
	if tip := r.n.s.sh.certTip(c.ClientCert); tip != "" {
		lines = append(lines, tip)
	}
	return strings.Join(lines, "\n")
}

// Tapped implements fyne.Tappable: the cluster's tree.
func (r *navContextRow) Tapped(*fyne.PointEvent) {
	r.n.s.sh.tip.hideFor(r)
	r.n.tapped(r.id)
}

// TappedSecondary implements fyne.SecondaryTappable: the cluster's menu.
func (r *navContextRow) TappedSecondary(ev *fyne.PointEvent) { r.n.rowMenu(r.id, ev.AbsolutePosition) }

// MouseIn implements desktop.Hoverable.
func (r *navContextRow) MouseIn(ev *desktop.MouseEvent) {
	r.hovered = true
	r.Refresh()
	r.n.s.sh.tip.hover(r, r.tipText(), ev.AbsolutePosition)
}

// MouseMoved implements desktop.Hoverable.
func (r *navContextRow) MouseMoved(ev *desktop.MouseEvent) {
	r.n.s.sh.tip.moved(r, ev.AbsolutePosition)
}

// MouseOut implements desktop.Hoverable.
func (r *navContextRow) MouseOut() {
	r.hovered = false
	r.Refresh()
	r.n.s.sh.tip.hideFor(r)
}

// CreateRenderer implements fyne.Widget.
func (r *navContextRow) CreateRenderer() fyne.WidgetRenderer {
	bar := newSelBar()
	rr := &rowBodyRenderer{w: r, body: r.body, bg: r.bg, ring: r.ring, bar: bar, paint: func() {
		paintRow(r.Theme(), r.bg, r.ring, r.n.marked(r.id), r.hovered, r.n.keyboardOn(r.id))
		paintSelBar(r.Theme(), bar, r.n.marked(r.id))
	}}
	rr.Refresh()
	return rr
}

// navSourceRow is a source group's heading: a chevron, the folder or file the group's clusters come
// from, and while collapsed how many it hides. A click collapses or opens the group (A63).
type navSourceRow struct {
	widget.BaseWidget
	n        *navSections
	group    string
	chevron  *widget.Icon
	label    *textLine
	count    *textLine
	body     fyne.CanvasObject
	bg, ring *canvas.Rectangle
	hovered  bool
	open     bool
	clusters int
}

var (
	_ fyne.Tappable     = (*navSourceRow)(nil)
	_ desktop.Hoverable = (*navSourceRow)(nil)
)

func newNavSourceRow(n *navSections, group string) *navSourceRow {
	r := &navSourceRow{n: n, group: group, open: true, bg: canvas.NewRectangle(color.Transparent), ring: canvas.NewRectangle(color.Transparent)}
	r.chevron = widget.NewIcon(themedIcon(theme.MoveDownIcon(), colorMuted))
	r.label = newTextLine(kubeconfig.DisplayText(group, maxShownPath), colorMuted, theme.SizeNameCaptionText, fyne.TextStyle{Bold: true})
	r.label.truncate = true
	r.count = newTextLine("", colorMuted, theme.SizeNameCaptionText, fyne.TextStyle{})
	r.body = inset(theme.SizeNamePadding, theme.SizeNamePadding, container.NewBorder(nil, nil, container.NewCenter(r.chevron), container.NewCenter(r.count),
		container.NewVBox(layout.NewSpacer(), r.label, layout.NewSpacer())))
	r.ExtendBaseWidget(r)
	return r
}

// set shows the group open or collapsed, with the number of its clusters the filter keeps.
func (r *navSourceRow) set(clusters int, open bool) {
	r.clusters = clusters
	if open != r.open {
		r.open = open
		icon := theme.MoveDownIcon() // Cogpit's chevron (theme.go)
		if !open {
			icon = iconChevronRight
		}
		r.chevron.SetResource(themedIcon(icon, colorMuted))
	}
	count := ""
	if !open {
		count = strconv.Itoa(clusters)
	}
	r.count.SetText(count)
	r.Refresh()
}

// tipText is the heading's tooltip: the whole source and how many clusters it holds.
func (r *navSourceRow) tipText() string {
	state := "open"
	if !r.open {
		state = "collapsed"
	}
	return kubeconfig.DisplayText(r.group, maxShownPath) + " - " + strconv.Itoa(r.clusters) + " cluster" + plural(r.clusters) + ", " + state
}

// Tapped implements fyne.Tappable: the group collapses or opens.
func (r *navSourceRow) Tapped(*fyne.PointEvent) {
	r.n.s.sh.tip.hideFor(r)
	r.n.tapped(navSourcePrefix + r.group)
}

// MouseIn implements desktop.Hoverable.
func (r *navSourceRow) MouseIn(ev *desktop.MouseEvent) {
	r.hovered = true
	r.Refresh()
	r.n.s.sh.tip.hover(r, r.tipText(), ev.AbsolutePosition)
}

// MouseMoved implements desktop.Hoverable.
func (r *navSourceRow) MouseMoved(ev *desktop.MouseEvent) { r.n.s.sh.tip.moved(r, ev.AbsolutePosition) }

// MouseOut implements desktop.Hoverable.
func (r *navSourceRow) MouseOut() {
	r.hovered = false
	r.Refresh()
	r.n.s.sh.tip.hideFor(r)
}

// CreateRenderer implements fyne.Widget.
func (r *navSourceRow) CreateRenderer() fyne.WidgetRenderer {
	rr := &rowBodyRenderer{w: r, body: r.body, bg: r.bg, ring: r.ring, paint: func() {
		paintRow(r.Theme(), r.bg, r.ring, false, r.hovered, r.n.keyboardOn(navSourcePrefix+r.group))
	}}
	rr.Refresh()
	return rr
}

// rowBodyRenderer draws a row's background, its body and its focus ring, painted by paint.
type rowBodyRenderer struct {
	w        fyne.Widget
	body     fyne.CanvasObject
	bg, ring *canvas.Rectangle
	bar      *canvas.Rectangle // the selection's bar (chrome_nav.go); nil for a row that is never the open one
	paint    func()
}

func (rr *rowBodyRenderer) MinSize() fyne.Size { return rr.body.MinSize() }

func (rr *rowBodyRenderer) Layout(size fyne.Size) {
	rr.body.Move(fyne.NewPos(0, 0))
	rr.body.Resize(size)
	layoutPlate(rr.bg, rr.ring, rr.bar, size)
}

func (rr *rowBodyRenderer) Refresh() {
	rr.paint()
	rr.Layout(rr.w.Size())
	rr.bg.Refresh()
	rr.ring.Refresh()
	rr.body.Refresh()
}

func (rr *rowBodyRenderer) Objects() []fyne.CanvasObject {
	objs := []fyne.CanvasObject{rr.bg, rr.body, rr.ring}
	if rr.bar != nil {
		objs = append(objs, rr.bar)
	}
	return objs
}

func (rr *rowBodyRenderer) Destroy() {}
