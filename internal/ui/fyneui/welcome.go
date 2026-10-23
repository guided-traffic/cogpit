package fyneui

import (
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"runtime"
	"slices"
	"strings"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/kubeconfig"
)

// The welcome page (ui-parity › Layout, "Welcome page (Help › Welcome)"; G5, G13). Lens has one
// behind Help › Welcome; its content is not captured, so the page, its wording and its layout are
// Cogpit's own (inferred, Cogpit's defaults 2026-10-13, not the owner's decision):
//   - Cogpit's logo, "Welcome to Cogpit" and a line saying what Cogpit is;
//   - a note about the connection when there is one to tell, in a banner of its own that names its
//     level (the owner's RW4, welcomenote.go): a connection Cogpit closed, a failed connect without
//     a context to name, or the cluster that stays connected under the page (a chosen cluster's
//     failed connect has its own page instead, errorpage.go, D-114);
//   - the clusters to connect with one click: the hotbar's, then Recent, the clusters of
//     this session (the connected or last one, the tabs' ones; the heading "Recent" since the
//     owner's A67, 2026-10-14 evening, was "This session"); each row as the context picker had it
//     (clusterrow.go), with its right-click menu. The "Other clusters" group that followed (up to
//     eight more the kubeconfig sources offer) is gone at the owner's AW13 of 2026-10-22 ("dass ich
//     auf der Welcome-Seite keine 'other Cluster' Sektion mehr haben will", D-114): the navigator's
//     section beside the page lists every cluster. Only on a first start, with no hotbar tile and
//     no recent cluster, the page lists them as "Your clusters" (up to eight, the kubeconfig's
//     current one first), so that it never offers nothing at all (the lead's detail, open to
//     revision);
//   - "Add Kubeconfigs…" (the Sync Files and Folders dialog) and below it the checkbox of the
//     preference "Restore the last session" (welcomeRestoreLabel), the documentation and the shortcuts
//     to remember, ⌘P finding a cluster by name among them. The page has no "Connect to Cluster…"
//     (the owner, 2026-10-16 evening, N122; D-57): its cluster rows and the navigator's section
//     beside it are the list already. It had one from 2026-10-15 (D-57; "All Clusters…" with the
//     context picker until then); the workspace's button without a live connection stays. The documentation is two links to
//     the repository on GitHub, opened in the browser (the owner's A113, 2026-10-14 evening): the
//     README for what Cogpit does (the showcase), and docs/developer/commands.md for the commands.
//
// Whether the page shows is engine state, UIState.Welcome (engine/welcome.go): the workspace shows it
// while no cluster is connected and while no tab is active, Help › Welcome (ui.welcome.open) over a
// connected cluster. It lies over the workspace and the dock, below the tab bar, so that a tab or a
// navigator item brings the views back (the settings pages take the navigator's place too). The
// tab bar holds it: the page is the workspace's state when no tab is active.

const (
	welcomeColumn    = 600 // the page's column, in points
	welcomeLogoSize  = 88  // the logo's side
	welcomeMaxRecent = 6   // clusters of this session listed
	welcomeMaxFirst  = 8   // clusters listed on a first start ("Your clusters", AW13)
	welcomeTopMargin = 12  // above the column, when the workspace is tall enough (the logo's glow brings its own)
	readmeURL        = "https://github.com/guided-traffic/cogpit#readme"
	commandsURL      = "https://github.com/guided-traffic/cogpit/blob/main/docs/developer/commands.md"
	welcomeTagline   = "Cogpit is a Kubernetes IDE: it shows your clusters from a live cache, asks before it changes anything, " +
		"and lets agents drive it over MCP."
)

// welcomePage is the welcome page. Main goroutine, except what says otherwise.
type welcomePage struct {
	sh   *shell
	root *fyne.Container // over the workspace and the dock while shown

	note      *welcomeNote    // the connection's message, in a callout of its own (RW4); hidden without one
	list      *fyne.Container // the groups of clusters
	hint      *textLine
	addB      *widget.Button
	restore   *widget.Check     // the preference "Restore the last session" (preferences_restore.go)
	syncing   bool              // set while restore follows the preference, so that it sends nothing
	docs      *widget.Hyperlink // the README: what Cogpit does
	cmds      *widget.Hyperlink // docs/developer/commands.md: every command
	shown     bool
	built     string         // the signature the list was built for
	rows      []*clusterRow  // the clusters' rows in the order shown, for tests
	titles    map[string]int // how many rows show each title; a shared one names its kubeconfig (sharedTitles)
	links     *clusterLinks  // the clusters' links, for Remove from Cogpit (menus_cluster.go)
	menuModel *fyne.Menu     // a row's open menu, for tests

	contexts []engine.ContextInfo // the contexts the sources offer, read off the main goroutine
	loadSeq  int
	loaded   bool
	pending  string // the context whose connect or login review a click began and that has not returned (connect)

	subOnce  sync.Once
	sigMu    sync.Mutex
	eventSig string // the last UI state event's signature (event goroutines)
}

func newWelcomePage(sh *shell) *welcomePage {
	w := &welcomePage{sh: sh}
	w.links = newClusterLinks(sh)
	title := newTextLine("Welcome to Cogpit", theme.ColorNameForeground, theme.SizeNameHeadingText, fyne.TextStyle{Bold: true})
	title.align = fyne.TextAlignCenter
	tagline := newCenteredText(welcomeTagline, colorMuted, theme.SizeNameText, welcomeColumn-120)
	// The head stands in the column's middle: the logo on its glow, the title and the tagline under it.
	head := container.NewVBox(container.NewCenter(newLogoGlow(welcomeLogoSize)), title, newSpace(theme.SizeNamePadding),
		container.NewCenter(tagline))
	w.note = newWelcomeNote()
	w.list = container.NewVBox()
	w.addB = widget.NewButtonWithIcon("Add Kubeconfigs…", theme.ContentAddIcon(), sh.kc.addKubeconfigs)
	u, _ := url.Parse(readmeURL)
	w.docs = widget.NewHyperlink("What Cogpit does: the README on GitHub", u)
	w.docs.OnTapped = func() { sh.openDocumentation() }
	cu, _ := url.Parse(commandsURL)
	w.cmds = widget.NewHyperlink("Every command, for the palette, the menus and agents: commands.md on GitHub", cu)
	w.cmds.OnTapped = func() { sh.openLink(commandsURL) }
	w.hint = newTextLine("", colorMuted, theme.SizeNameCaptionText, fyne.TextStyle{})
	w.restore = widget.NewCheck(welcomeRestoreLabel, w.setRestore)
	column := container.NewVBox(
		head,
		newSpace(theme.SizeNameInnerPadding),
		w.note.root,
		newSpace(theme.SizeNameInnerPadding),
		w.list,
		container.NewHBox(w.addB),
		w.restore,
		newSpace(theme.SizeNameInnerPadding),
		heading("Documentation"),
		w.docs,
		w.cmds,
		w.hint,
	)
	page := box(welcomeLayout{}, column)
	// The horizon is the page's backdrop: no data lies under it, only the stage light and the grid floor.
	w.root = newWelcomeStage(page) // the horizon behind the page, its grid floor masked under the content
	w.root.Hide()
	return w
}

// welcomeRestoreLabel is the welcome page's checkbox of the preference "Restore the last session"
// (Preferences › App, D-69 item 8), which the owner missed on the page (walk 2026-10-17, WF1: "eine
// kleine Box …, die anbietet den letzten Zustand von Cogpit beim Beenden zu speichern und beim
// Wiederöffnen wiederherzustellen"). The same preference, not a second one: on or off here is on or
// off in the Preferences. It shows whether it is on or off, under "Add Kubeconfigs…", where the page
// lists what a start offers (the lead's decision, 2026-10-17 evening).
const welcomeRestoreLabel = "Restore this session at the next start: its tabs and connected clusters"

// renderRestore shows the preference in the checkbox.
func (w *welcomePage) renderRestore() {
	on := engine.DefaultPreferences().RestoreSession
	if pr := w.sh.ui.Preferences; pr != nil {
		on = pr.RestoreSession
	}
	w.syncing = true
	w.restore.SetChecked(on)
	w.syncing = false
}

// setRestore writes the preference when the user ticks or unticks the box.
func (w *welcomePage) setRestore(on bool) {
	if w.syncing {
		return
	}
	w.sh.dispatchInOrder("preferences.set", map[string]any{"restoreSession": on}, func(_ any, err error) {
		if err != nil {
			w.sh.toast("error", "Could not change Restore the last session: "+err.Error())
		}
	})
}

// workspace is the tab bar's hook into the window's centre (newShell): the workspace with the dock,
// and the welcome page or a failed connect's page (errorpage.go) over both while it shows.
func (b *tabBar) workspace(body fyne.CanvasObject) fyne.CanvasObject {
	return container.NewStack(body, b.welcome.root, b.errPage.root)
}

// sync follows UIState.Welcome: shows or hides the page, and refreshes what it lists. The tab bar
// calls it whenever the tabs, the connection or a cluster's identity change.
func (w *welcomePage) sync() {
	w.subscribe()
	show := w.sh.ui.Welcome
	if show != w.shown {
		w.shown = show
		if show {
			w.sh.tip.hide()
			w.sh.win.Canvas().Unfocus() // no key may reach a table or a field the page hides
			w.root.Show()
			w.load()
		} else {
			w.root.Hide()
		}
	}
	if show {
		w.render()
	}
}

// subscribe follows what the page lists beyond the tab bar's changes, for as long as the shell
// runs: the contexts (a kubeconfig synced or removed) and the hotbar. It never touches a widget on
// the engine's goroutine.
func (w *welcomePage) subscribe() {
	w.subOnce.Do(func() {
		cancel := w.sh.eng.Subscribe(func(ev engine.Event) {
			switch ev.Type {
			case engine.EventContexts:
				w.sh.do(func() {
					if w.shown {
						w.load()
					}
				})
			case engine.EventUIState:
				if ev.UIState == nil {
					return
				}
				sig := welcomeStateSig(*ev.UIState)
				w.sigMu.Lock()
				changed := sig != w.eventSig
				w.eventSig = sig
				w.sigMu.Unlock()
				if changed {
					w.sh.do(func() {
						if w.shown {
							w.render()
						}
					})
				}
			}
		})
		go func() {
			<-w.sh.ctx.Done()
			cancel()
		}()
	})
}

// welcomeStateSig is what of a UI state the page's list depends on beyond the tab bar's changes:
// the hotbar's tiles, the tabs' clusters and the connected clusters' marks (D-69).
func welcomeStateSig(st engine.UIState) string {
	var b strings.Builder
	for _, c := range st.Connections {
		fmt.Fprintf(&b, "%s=%s/%v/%v|", c.Context, c.Connection.State, c.Cold, c.LoginLapsed)
	}
	if h := st.Hotbar; h != nil {
		raw, _ := json.Marshal(h)
		b.Write(raw)
	}
	for _, t := range st.Tabs {
		b.WriteString("|" + t.Context)
	}
	return b.String()
}

// reload reads the contexts again while the page shows (after a connect ended: its failed
// connects, ContextInfo.ConnectProblem).
func (w *welcomePage) reload() {
	if w.shown {
		w.load()
	}
}

// load reads the contexts and their links off the main goroutine, then renders. A newer load
// supersedes an older one.
func (w *welcomePage) load() {
	w.loadSeq++
	seq := w.loadSeq
	go func() {
		defer w.sh.recoverTo("the welcome page")
		ctxs, _ := w.sh.eng.Contexts() // a partial list is shown; the navigator's section says what failed
		links := w.links.read()
		w.sh.do(func() {
			if seq != w.loadSeq {
				return
			}
			w.contexts, w.loaded = ctxs, true
			w.links.set(links)
			w.built = "" // the rows' contexts may have changed under the same ids
			if w.shown {
				w.render()
			}
		})
	}()
}

// welcomeGroup is one group of clusters on the page.
type welcomeGroup struct {
	title    string
	contexts []engine.ContextInfo
}

// groups are the clusters to list (see the top of the file). The hotbar and the tabs come from the
// engine's state as it is now: a hotbar change reaches the page through its own subscription,
// before the shell applies it.
func (w *welcomePage) groups() []welcomeGroup {
	st := w.sh.eng.UIState()
	byName := make(map[string]engine.ContextInfo, len(w.contexts))
	for _, c := range w.contexts {
		byName[c.Name] = c
	}
	seen := map[string]bool{}
	take := func(g *welcomeGroup, id string) {
		if c, ok := byName[id]; ok && !seen[id] {
			seen[id] = true
			g.contexts = append(g.contexts, c)
		}
	}
	var out []welcomeGroup
	if h := st.Hotbar; h != nil {
		g := welcomeGroup{title: "Hotbar"}
		for _, t := range h.Clusters {
			take(&g, t.Context)
		}
		if len(g.contexts) > 0 {
			out = append(out, g)
		}
	}
	recent := welcomeGroup{title: "Recent"}
	ids := make([]string, 0, 1+len(st.Tabs))
	ids = append(ids, st.Context)
	for _, t := range st.Tabs {
		ids = append(ids, t.Context)
	}
	for _, id := range ids {
		if len(recent.contexts) < welcomeMaxRecent {
			take(&recent, id)
		}
	}
	if len(recent.contexts) > 0 {
		out = append(out, recent)
	}
	if len(out) > 0 {
		return out // no "Other clusters" (AW13): the navigator's section lists every cluster
	}
	// A first start: the clusters the kubeconfigs offer, their current context first.
	all := welcomeGroup{title: "Your clusters"}
	for _, c := range w.contexts {
		if c.Current {
			take(&all, c.Name)
		}
	}
	for _, c := range w.contexts {
		if len(all.contexts) < welcomeMaxFirst {
			take(&all, c.Name)
		}
	}
	if len(all.contexts) > 0 {
		out = append(out, all)
	}
	return out
}

// render shows the connection's line and the clusters. The list is rebuilt only when what it shows
// changed.
func (w *welcomePage) render() {
	defer welcomeStageOf(w.root).relayout() // the scrim over the grid floor ends under the last line
	w.renderStatus()
	w.renderHint()
	w.renderRestore()
	groups := w.groups()
	ui := w.sh.ui
	var sig strings.Builder
	sig.WriteString(string(ui.Connection.State) + "|" + ui.Context)
	for _, c := range ui.Connections { // the connected clusters' marks (D-69)
		fmt.Fprintf(&sig, "|%s=%s/%v/%v", c.Context, c.Connection.State, c.Cold, c.LoginLapsed)
	}
	for _, g := range groups {
		sig.WriteString("|" + g.title)
		for _, c := range g.contexts {
			id := w.sh.identity(c.Name)
			sig.WriteString("|" + c.Name + "/" + id.name + "/" + string(id.ink) + "/" + c.Exec)
			if id.image != nil {
				sig.WriteString("/img")
			}
			if c.ConnectProblem {
				sig.WriteString("/!")
			}
			if text, color := certPill(c.ClientCert, w.sh.now()); text != "" { // the clock moves it (D-75)
				sig.WriteString("/" + text + "/" + string(color))
			}
		}
	}
	if !w.loaded {
		sig.WriteString("|loading")
	}
	if sig.String() == w.built {
		return
	}
	w.built = sig.String()
	w.rows = w.rows[:0]
	var objs []fyne.CanvasObject
	w.titles = w.sharedTitles(groups)
	for _, g := range groups {
		objs = append(objs, newBracketLabel(headText(g.title, len(g.contexts)), colorAccent), newSpace(theme.SizeNamePadding))
		for _, c := range g.contexts {
			objs = append(objs, plateOf(w.row(c)))
		}
		objs = append(objs, newSpace(theme.SizeNamePadding))
	}
	if len(objs) == 0 {
		msg := "No clusters yet. Add a kubeconfig file, or a folder of them: Cogpit reads them and never writes them."
		if !w.loaded {
			msg = "Reading your kubeconfigs…"
		}
		objs = append(objs, newParagraph(msg, colorMuted, welcomeColumn), newSpace(theme.SizeNamePadding))
	}
	w.list.Objects = objs
	w.list.Refresh()
}

// labels are a cluster row's title and subtitle: the one name of the cluster (its display name,
// else its short name, D-68), the context's full name below it where they differ.
func (w *welcomePage) labels(c engine.ContextInfo) (title, sub string) {
	sh := w.sh
	title, sub = contextLabels(c)
	named := sh.ui.ClusterSettings[c.Name].Name != "" // the demo cluster keeps its label unless the user named it
	if name := sh.identity(c.Name).name; (!c.Demo || named) && kubeconfig.DisplayText(name, maxShownPath) != title {
		sub = strings.TrimSuffix("context "+title+" · "+sub, " · ")
		title = kubeconfig.DisplayText(name, maxShownPath)
	}
	return title, sub
}

// sharedTitles counts the titles the page's rows show: a row whose title another row shows too
// names its kubeconfig file, so that the two can be told apart; every other row leaves the file
// out (the owner, 2026-10-21: the path only for the clusters whose names occur more than once).
func (w *welcomePage) sharedTitles(groups []welcomeGroup) map[string]int {
	n := map[string]int{}
	for _, g := range groups {
		for _, c := range g.contexts {
			title, _ := w.labels(c)
			n[title]++
		}
	}
	return n
}

// row is one cluster, connecting with one click.
func (w *welcomePage) row(c engine.ContextInfo) *clusterRow {
	sh := w.sh
	title, sub := w.labels(c)
	if w.titles[title] < 2 {
		c.Source = "" // its name is unique on the page: no file pill
	}
	name := c.Name
	connect := func() { w.connect(name) }
	r := newClusterRow(c, title, sub, connect, func() { sh.kc.showConnectLog(name) })
	r.badge.setIdentity(sh.identity(name))
	if text, color := certPill(c.ClientCert, sh.now()); text != "" { // D-75, clientcert.go
		objs := r.badges.Objects
		if r.warn != nil { // before the failed connect's icon, which stays last
			r.badges.Objects = slices.Insert(objs, len(objs)-1, fyne.CanvasObject(newPill(text, color, true)))
		} else {
			r.badges.Add(newPill(text, color, true))
		}
	}
	if c := sh.ui.Connection; (sh.ui.Context == name && (c.State.Live() || c.State == engine.Connecting)) || clusterConnected(sh.ui, name) {
		d := newConnDot(sh) // its mark, never a word (F13, D-69): connecting, connected, warm, cold, reconnecting, login expired
		d.setMark(name, sh.mark(name, false))
		r.badges.Add(d)
	}
	r.onMenu = func(pos fyne.Position) { w.menu(name, pos) }
	w.rows = append(w.rows, r)
	return r
}

// connect connects a cluster as the navigator's section does: a login program the user has not
// allowed yet is shown first; the connection's outcome shows on the page (or the cluster's tab
// replaces it). A connected cluster is only shown (D-69). A click on a cluster whose connect is
// under way, the page's own (pending, from the click to the engine's answer, the login program's
// review included) or the one the window shows connecting, does nothing more: the second click of
// a double click, or an impatient one, would ask a second review, or send a second cluster.connect
// that joins the run under way and ends the first one's wait with an error (D-106, AW12).
func (w *welcomePage) connect(name string) {
	sh := w.sh
	if w.pending == name || (sh.ui.Context == name && sh.ui.Connection.State == engine.Connecting) {
		return
	}
	if sh.ui.Context == name && sh.ui.Connection.State.Live() {
		if len(sh.ui.Tabs) > 0 { // under Help › Welcome: back to the cluster's last tab
			for _, t := range sh.ui.Tabs {
				if t.Context == name {
					sh.dispatch("ui.tab.select", tabSelectParams{ID: t.ID}, nil)
					return
				}
			}
		}
		sh.dispatch("ui.openView", openViewParams{View: kindPods}, nil)
		return
	}
	for _, c := range w.contexts {
		if c.Name == name && (c.Exec == engine.ExecRequired || c.Exec == engine.ExecChanged) && !switchable(sh.ui, name) {
			w.pending = name
			sh.kc.reviewPlugin(name, func(allowed bool) {
				if allowed {
					w.dispatchConnect(name)
				} else if w.pending == name {
					w.pending = ""
				}
			})
			return
		}
	}
	w.dispatchConnect(name)
}

// dispatchConnect sends cluster.connect for name, pending until the engine answers; an error is a
// toast, as dispatch shows it.
func (w *welcomePage) dispatchConnect(name string) {
	sh := w.sh
	w.pending = name
	sh.dispatch("cluster.connect", contextParams{Context: name}, func(_ any, err error) {
		if w.pending == name {
			w.pending = ""
		}
		if sh.ctx.Err() == nil {
			sh.showError(err)
		}
	})
}

// menu is a cluster row's right-click menu: Connect, Settings, the hotbar's item, Disconnect and
// Remove from Cogpit, as the navigator's section rows have them.
func (w *welcomePage) menu(name string, pos fyne.Position) {
	sh := w.sh
	items := []*fyne.MenuItem{fyne.NewMenuItem(connectItemLabel(sh.ui, name), func() { w.connect(name) })}
	if item := sh.settingsItem(name); item != nil {
		items = append(items, item)
	}
	if item := sh.hot.contextItem(name); item != nil {
		items = append(items, item)
	}
	items = append(items, sh.clusterItems(name)...)
	sh.tip.hide()
	w.menuModel = fyne.NewMenu("", items...)
	widget.NewPopUpMenu(w.menuModel, sh.win.Canvas()).ShowAtPosition(pos)
}

// renderStatus shows what there is to say about the connection, in its note (welcomenote.go): a
// failed connect (error), a connection Cogpit closed (warning), or the cluster still connected under
// the page (info).
func (w *welcomePage) renderStatus() {
	ui := w.sh.ui
	name := ""
	if ui.Context != "" {
		name = w.sh.identity(ui.Context).name
	}
	title, text, level := "", "", noteInfo
	switch ui.Connection.State {
	case engine.ConnectError: // without a context to name: a chosen cluster's failure has its page (errorpage.go)
		title, text, level = "Could not connect", ui.Connection.Error, noteError
		if name != "" {
			title = "Could not connect to " + name
		}
	case engine.Disconnected:
		if ui.Connection.Error != "" { // Cogpit closed it: its kubeconfig file went away
			title, text, level = "Connection closed", ui.Connection.Error, noteWarning
		}
	case engine.Connected:
		title, text = "Connected to "+name, "Pick a view in the navigator, or a tab above."
	}
	w.note.set(level, title, text)
}

// renderHint names the shortcuts worth knowing, as the registry declares them.
func (w *welcomePage) renderHint() {
	var parts []string
	for _, k := range []struct{ id, what string }{
		{"cluster.connect", "finds a cluster by name"}, {engine.PaletteOpenCommand, "runs any command"},
		{"preferences.open", "opens the Preferences"},
	} {
		if c, ok := w.sh.eng.Registry().Get(k.id); ok && c.Shortcut != "" {
			parts = append(parts, shortcutText(c.Shortcut, runtime.GOOS == goosMac)+" "+k.what)
		}
	}
	w.hint.SetText(strings.Join(parts, " · "))
}

// openDocumentation opens Cogpit's README on GitHub in the browser (Help › Documentation and the
// welcome page). A GUI link, not an engine command, as the status bar's link (D-23): no agent opens
// a browser on the user's computer.
func (sh *shell) openDocumentation() { sh.openLink(readmeURL) }

// openLink opens one of Cogpit's own https links in the browser: the README, the command list.
func (sh *shell) openLink(link string) {
	u, _ := url.Parse(link)
	if err := openBrowser(sh.app, u); err != nil {
		sh.toast("error", "Could not open "+link+": "+err.Error())
	}
}

// welcomeLayout puts the page's column at the top centre, welcomeColumn wide (narrower in a narrow
// workspace), with a margin above it while the workspace is tall enough.
type welcomeLayout struct{}

func (welcomeLayout) MinSize(objs []fyne.CanvasObject) fyne.Size {
	ms := fyne.NewSize(0, 0)
	for _, o := range objs {
		ms = ms.Max(o.MinSize())
	}
	pad := fyne.CurrentApp().Settings().Theme().Size(theme.SizeNameInnerPadding)
	return ms.AddWidthHeight(2*pad, 2*pad+welcomeTopMargin)
}

func (welcomeLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	pad := fyne.CurrentApp().Settings().Theme().Size(theme.SizeNameInnerPadding)
	for _, o := range objs {
		ms := o.MinSize()
		w := float32(math.Max(float64(ms.Width), math.Min(welcomeColumn, float64(size.Width-2*pad))))
		x := float32(math.Max(float64(pad), float64(size.Width-w)/2))
		o.Move(fyne.NewPos(x, pad+welcomeTopMargin))
		o.Resize(fyne.NewSize(w, ms.Height))
	}
}

// welcomeLogo is Cogpit's logo at a large size (the welcome page, About): the current version's
// tile (internal/ui/logo, D-24), drawn at the display's pixel size (pixelsFor).
type welcomeLogo struct {
	widget.BaseWidget
	side float32
	px   int
}

func newWelcomeLogo(side float32) *welcomeLogo {
	l := &welcomeLogo{side: side}
	l.ExtendBaseWidget(l)
	return l
}

// CreateRenderer implements fyne.Widget.
func (l *welcomeLogo) CreateRenderer() fyne.WidgetRenderer {
	img := canvas.NewImageFromImage(nil)
	img.FillMode, img.ScaleMode = canvas.ImageFillContain, canvas.ImageScaleSmooth
	r := &welcomeLogoRenderer{l: l, img: img}
	r.Refresh()
	return r
}

type welcomeLogoRenderer struct {
	l   *welcomeLogo
	img *canvas.Image
}

func (r *welcomeLogoRenderer) MinSize() fyne.Size { return fyne.NewSize(r.l.side, r.l.side) }

func (r *welcomeLogoRenderer) Layout(size fyne.Size) {
	s := r.l.side
	r.img.Move(fyne.NewPos((size.Width-s)/2, (size.Height-s)/2))
	r.img.Resize(fyne.NewSize(s, s))
}

func (r *welcomeLogoRenderer) Refresh() {
	l := r.l
	px := pixelsFor(fyne.CurrentApp().Driver().CanvasForObject(l), l.side)
	if px != l.px || r.img.Image == nil {
		r.img.Image = logoTile(px)
		l.px = px
	}
	r.Layout(l.Size())
	r.img.Refresh()
}

func (r *welcomeLogoRenderer) Objects() []fyne.CanvasObject { return []fyne.CanvasObject{r.img} }
func (r *welcomeLogoRenderer) Destroy()                     {}

// shownClusters are the contexts of the page's rows in their order (tests).
func (w *welcomePage) shownClusters() []string {
	out := make([]string, 0, len(w.rows))
	for _, r := range w.rows {
		out = append(out, r.title.text)
	}
	return out
}
