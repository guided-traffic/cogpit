package fyneui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/ui/nativedialog"
)

// Timings of the shell. They are behaviour, not styling, so they live here and not in the theme.
const (
	searchDebounce   = 150 * time.Millisecond // typing pause before the filter is sent
	ageRefresh       = 10 * time.Second       // how often age cells are re-rendered
	dockOffset       = 0.66                   // the workspace's share of the height while the dock is open
	diagnosticsEvery = 250 * time.Millisecond
)

// shell is the window's content and the glue between engine state and widgets.
//
// Threading: every field without a comment saying otherwise is touched only on Fyne's main
// goroutine. Engine events arrive on engine goroutines and are handed over with sh.do; commands
// are dispatched from goroutines, never from the main goroutine, because a command may wait for
// the network or for a confirmation that itself needs the main goroutine.
type shell struct {
	eng  Engine
	sess *engine.Session // the real session, for Command.Enabled; nil with test fakes
	// orderMu guards orderTail, the end of the dispatches that run in order (dispatchInOrder).
	orderMu   sync.Mutex
	orderTail chan struct{}
	app       fyne.App
	win       fyne.Window
	opts      Options
	// inlineMenus: the menus are drawn in the top bar (Windows and Linux, menubar.go); the window's
	// main menu, menuKeys, only holds them for their keys (menuItems, the last built), installed
	// once the window exists (deferMenuKeys until then).
	inlineMenus   bool
	menuKeys      *fyne.MainMenu
	menuItems     []*fyne.Menu
	deferMenuKeys bool
	// do runs a function on the main goroutine (fyne.Do, a queue in tests); once the shell stopped,
	// what it queued before and what arrives after is dropped (stopped).
	do      func(func())
	stopped atomic.Bool
	now     func() time.Time
	// saveFile asks where to save a file and writes it (a file save dialog; tests record it).
	saveFile func(name string, rows int, data []byte)
	// files shows the system's file panels (filepicker.go): none until Run puts macOS's in place,
	// a recorder in tests. bookmarks keeps what the panels grant for later runs (Options.Bookmarks).
	files     filePicker
	bookmarks *nativedialog.Store

	ctx    context.Context // ends when the window closes; bounds every dispatch
	cancel context.CancelFunc

	ui         engine.UIState // the state the widgets show
	uiApplied  bool
	kinds      []kindInfo
	kindsFor   string // the context kinds were loaded for
	kindsSeq   int    // the latest kinds.list request; older results are dropped
	namespaces []string
	nsFollow   map[int]func([]string) // open fields that offer the namespaces (followNamespaces)
	nsFollowID int
	// nsFor is the context namespaces were read for; nsTerminating its namespaces being deleted
	// (nsgone.go, GW9).
	nsFor         string
	nsTerminating map[string]bool
	diag          diagView

	// Coalescing of engine events: the latest UI state wins, and at most one table, detail and
	// diagnostics refresh is queued at a time. Guarded by pendingMu or atomic.
	pendingMu     sync.Mutex
	pendingUI     *engine.UIState
	tableQueued   atomic.Bool
	detailQueued  atomic.Bool
	metricsQueued atomic.Bool
	kindsQueued   atomic.Bool
	diagQueued    atomic.Bool
	lastDiagFetch atomic.Int64 // UnixNano of the last diagnostics fetch

	top       *topBar
	side      *sidebar
	view      *tableView
	drawer    *drawer
	dock      *dock
	status    *statusBar
	toasts    *fyne.Container
	tip       *tooltip      // the one tooltip: cut-off cells, warning icons, drawer actions
	palette   *cmdPalette   // the command palette, ⌘⇧P (palette.go)
	hot       *hotbar       // the pinned clusters at the window's left edge (hotbar.go)
	tabs      *tabBar       // the tabs of cluster views above the workspace (tabbar.go)
	kc        *kubeconfigUI // the kubeconfig sources' dialogs (kubeconfigs.go)
	blink     *blinker      // the connection dots' one clock (conndot.go, F13)
	settings  *settingsPage // Preferences and Cluster settings (preferences_page.go)
	confirmer *guiConfirmer
	conn      *connBanner  // an interrupted connection or an expired login, right below the top bar (connbanner.go, D-58)
	drains    *drainBanner // unfinished drains, below the top bar (drains.go)
	helm      *helmBanner  // unfinished Helm release actions, below the drains (helmbanner.go)
	titlebar  *titlebar    // Cogpit's own title bar on macOS (titlebar.go)

	drafts *draftsBanner // unsaved editor text kept from an earlier run, below the Helm banner (drafts.go)

	helmHTTP *helmHTTPBanner // Helm repositories waiting to send credentials over http, below the Helm banner (helmhttp.go, H-40)

	intq *integrationsQuestion // the question of a new cluster's integrations (integrationsask.go, D-76)

	hsplit    *fyne.Container // table | drawer, the drawer's width set by the user (panelwidths.go)
	mainRow   *fyne.Container // sidebar | body, the sidebar's width set by the user
	center    *fyne.Container // mainRow, or the open settings page in its place
	sidebarW  float32         // the user's widths of the two panels, stored in the preferences
	drawerW   float32
	sideLay   *panelLayout
	drawerLay *panelLayout
	chat      *chatPanel       // the agent's chat column at the window's right edge (chat.go)
	chatW     float32          // the user's width of the chat column, stored in the preferences
	chatLay   *panelLayout     // the chat column beside chatRow's work area
	chatSplit *fyne.Container  // the work area | the chat column, while the chat is shown
	chatRow   *fyne.Container  // the work area alone, or chatSplit
	vsplit    *container.Split // workspace / dock
	workspace *fyne.Container  // the table view, or hsplit while the drawer is open
	body      *fyne.Container  // the workspace, with the dock below it
	root      fyne.CanvasObject

	workArea fyne.CanvasObject // the workspace with the welcome page over it, not over the dock (auditlog.go)

	// A Secret's value copied from the drawer is cleared from the clipboard after secretClipTTL
	// (copy.go); main goroutine only.
	clipTimer     *time.Timer
	clipSecret    string
	secretClipTTL time.Duration
	// copyConfirm is how long the copy icon shows a checkmark after a copy (a test shortens it).
	copyConfirm time.Duration
	// toastTime, when set, replaces every toast's lifetime (a test shortens it).
	toastTime time.Duration
	// setConcealed puts a Secret's value on the system clipboard marked for clipboard managers to
	// skip (clipboard_darwin.go); nil in tests, which use Fyne's test clipboard and must not touch
	// the system's. It reports false when it could not, and Fyne's clipboard is used.
	setConcealed func(string) bool
	// menuKey reports whether the main menu runs an item for its key rather than a click
	// (menukeys.go): macOS's key equivalent; a test replaces it.
	menuKey func() bool

	unsubscribe  func()
	unwatchStray func() // a development build's toasts for Fyne's thread-check warnings (threadcheck.go)
	// early are the notices the engine gave before the window subscribed (a restored tab bar's,
	// D-41), shown as toasts with the start's warning once the window is up (showStartNotices).
	early       []engine.Notification
	stopTickers chan struct{}
	perf        *perfMeter

	mcp     mcpState       // the MCP server as the window knows it (mcp.go)
	mcpMenu *fyne.MenuItem // Help › Copy MCP command, disabled while the server is off

	helmRepos *helmReposDialog // the open Helm repositories dialog (helmcharts.go)
	// zone is the Preferences' time zone for the charts; nil: the clock's (preferences.go).
	zone *time.Location
	// icons are the clusters' uploaded icons by file, nil for one that cannot be read;
	// iconLoading the files being read (clusteridentity.go).
	icons       map[string]fyne.Resource
	iconLoading map[string]bool
	// charts are the Helm charts' icons the window fetched (helmcharticons.go, D-46).
	charts chartIconCache
	// systemReduce reads macOS's Reduce motion (reducemotion_darwin.go); tests replace it, so that
	// the computer's setting does not reach them (shell.reduceMotion, preferences_motion.go).
	systemReduce func() bool
}

// newShell builds every widget. Nothing talks to the engine until start.
func newShell(eng Engine, a fyne.App, w fyne.Window, opts Options, do func(func())) *shell {
	sh := &shell{eng: eng, app: a, win: w, opts: opts, now: time.Now, stopTickers: make(chan struct{}), secretClipTTL: secretClipboardTTL, copyConfirm: copyConfirmTime,
		menuKey: platformMenuKey}
	// Work that reaches the main goroutine after stop would apply engine state to a window that
	// is gone, possibly under another app and theme (tests run shells one after another).
	sh.do = func(fn func()) {
		do(func() {
			if !sh.stopped.Load() {
				fn()
			}
		})
	}
	sh.mcp = mcpState{on: opts.MCP == nil && opts.MCPCommand != "", command: opts.MCPCommand}
	sh.sess, _ = eng.(*engine.Session)
	sh.ctx, sh.cancel = context.WithCancel(context.Background())
	sh.files, sh.bookmarks = noPanels{}, opts.Bookmarks
	sh.saveFile = sh.saveTable // the system's save panel, else Fyne's dialog (filepicker_choosers.go)
	sh.titlebar = newTitlebar(sh)
	sh.loadWidths() // before the drawer, which wraps its values to the width it will have

	sh.systemReduce = systemReduceMotion
	sh.blink = newBlinker(sh) // before the bars, whose dots it refreshes
	sh.inlineMenus = opts.menus.inBar()
	sh.top = newTopBar(sh)
	sh.side = newSidebar(sh)
	sh.view = newTableView(sh)
	sh.drawer = newDrawer(sh)
	sh.dock = newDock(sh)
	sh.status = newStatusBar(sh)
	sh.kc = newKubeconfigUI(sh)
	sh.palette = newCmdPalette(sh)
	sh.settings = newSettingsPage(sh)
	sh.intq = newIntegrationsQuestion(sh)
	sh.confirmer = newGUIConfirmer(sh)
	sh.conn = newConnBanner(sh)
	sh.drains = newDrainBanner(sh)
	sh.helm = newHelmBanner(sh)
	sh.helmHTTP = newHelmHTTPBanner(sh)
	sh.drafts = newDraftsBanner(sh)
	sh.toasts = box(&toastLayout{})
	sh.tip = newTooltip(sh)
	sh.hot = newHotbar(sh)
	sh.tabs = newTabBar(sh)

	sh.drawerLay = &panelLayout{want: func() float32 { return sh.drawerW }, lo: minDrawerWidth, hi: maxDrawerWidth, restMin: minTableRoom,
		onGot: func(float32) { sh.do(sh.drawer.widthChanged) }}
	sh.hsplit = newPanel(sh.drawerLay, sh.drawer.root, sh.view.root, newResizeHandle(-1,
		func() float32 { return sh.drawerLay.got },
		func(w float32) float32 { return sh.setDrawerWidth(w) },
		sh.saveWidths))
	sh.vsplit = container.NewVSplit(widget.NewLabel(""), widget.NewLabel(""))
	sh.vsplit.SetOffset(dockOffset)
	sh.workspace = container.NewStack(sh.view.root)
	// The welcome page lies over the workspace, not the dock: a dock tab of no cluster (the Audit
	// Log) shows while no cluster is connected.
	sh.workArea = sh.tabs.workspace(sh.workspace)
	sh.body = container.NewStack()
	sh.layoutBody()

	sidebar := box(lockedWidth{}, newSurface(colorSidebar), sh.side.root)
	sh.sideLay = &panelLayout{first: true, want: func() float32 { return sh.sidebarW }, lo: minSidebarWidth, hi: maxSidebarWidth, restMin: minTableRoom}
	main := newPanel(sh.sideLay, sidebar, container.NewBorder(sh.tabs.root, nil, nil, nil, sh.body), newResizeHandle(1,
		func() float32 { return sh.sideLay.got },
		func(w float32) float32 { return sh.setSidebarWidth(w) },
		sh.saveWidths))
	sh.mainRow = main
	sh.center = container.NewStack(main)
	// The agent's chat column, the full height beside the work area, a settings page included
	// (chat.go); its width is the user's, as the sidebar's and the drawer's.
	sh.chat = newChatPanel(sh)
	sh.chatLay = &panelLayout{want: func() float32 { return sh.chatW }, lo: minChatWidth, hi: maxChatWidth,
		restMin: minSidebarWidth + handleWidth + minTableRoom}
	sh.chatSplit = newPanel(sh.chatLay, sh.chat.root, sh.center, newResizeHandle(-1,
		func() float32 { return sh.chatLay.got },
		func(w float32) float32 { return sh.setChatWidth(w) },
		sh.saveWidths))
	sh.chatRow = container.NewStack(sh.center)
	frame := container.NewBorder(
		container.NewVBox(box(layout.NewCustomPaddedVBoxLayout(0), sh.top.root, sh.top.edge), // the line directly under the bar
			sh.conn.root, sh.drains.root, sh.helm.root, sh.helmHTTP.root, sh.drafts.root),
		box(layout.NewCustomPaddedVBoxLayout(0), newDivider(false), sh.status.root), // the line directly on the bar
		sh.hot.root, nil, sh.chatRow)
	sh.root = container.NewStack(frame, sh.hot.drag.float, sh.tip.layer, sh.toasts) // a dragged hotbar tile over the window (hotbardrag.go)
	return sh
}

// start subscribes to the engine, shows the current state and connects the --context cluster, if
// any. Without one the workspace shows the welcome page and the navigator its section, and nothing
// connects by itself (D-42), a restored tab included (D-41).
func (sh *shell) start() {
	sh.early = sh.earlyNotices() // before subscribing: what comes after arrives as events
	sh.unsubscribe = sh.eng.Subscribe(sh.onEvent)
	sh.unwatchStray = sh.watchStrayCalls()
	sh.applyUIState(sh.eng.UIState())
	sh.requestDiagnostics()
	sh.installShortcuts()
	go sh.tick()

	if name := sh.opts.AutoConnect; name != "" {
		sh.dispatch("cluster.connect", contextParams{Context: name}, sh.autoConnected(name))
	}
}

// autoConnected answers the start's connect of --context name. A context the engine refuses (no
// such context, a login not allowed yet) says why in a toast and shows the navigator's section with
// its search focused, as ⌘P does (D-57): there the cluster can be connected, its login program
// allowed first. A connect that fails says why on the welcome page; one that did not fail
// (engine.ConnectNotFailed) says nothing.
func (sh *shell) autoConnected(name string) func(any, error) {
	return func(_ any, err error) {
		if err != nil && !engine.ConnectNotFailed(err) {
			sh.toast("error", "Could not connect to "+name+": "+err.Error())
			sh.findCluster()
		}
	}
}

// earlyNotices returns the unread notifications the engine logged before the window subscribed,
// oldest first; none from an engine that keeps no log (the tests' fake).
func (sh *shell) earlyNotices() []engine.Notification {
	if sh.sess == nil {
		return nil
	}
	var out []engine.Notification
	for _, n := range sh.sess.Notifications(true).Notifications { // newest first
		if !n.Read {
			out = append([]engine.Notification{n}, out...)
		}
	}
	return out
}

// showStartNotices shows, once the window is up, the warning the start brought (a crash report the
// last run left) and the notices the engine gave before the window subscribed (the tabs of the last
// run not restored, an unreadable tabs file), each closing marks its notification read.
func (sh *shell) showStartNotices() {
	if sh.opts.StartWarning != "" {
		sh.toast("warning", sh.opts.StartWarning)
	}
	for _, n := range sh.early {
		sh.showToast(n.Level, n.Message, n.ID)
	}
	sh.early = nil
	sh.mcpStartNotice() // the MCP server's kept port was taken (mcp.go)
}

// stop ends everything start began: the subscription, tickers, dispatches and dock streams, and
// drops the main-goroutine work still queued.
func (sh *shell) stop() {
	sh.stopped.Store(true)
	if sh.unsubscribe != nil {
		sh.unsubscribe()
	}
	if sh.unwatchStray != nil {
		sh.unwatchStray()
	}
	select {
	case <-sh.stopTickers:
	default:
		close(sh.stopTickers)
	}
	sh.cancel()
	sh.dock.closeAll()
	sh.titlebar.stop()
}

// tick re-renders age cells every ageRefresh, so "5m" becomes "6m" without an engine event, and
// the client certificates' time left in the cluster lists (D-75, clientcert.go).
func (sh *shell) tick() {
	t := time.NewTicker(ageRefresh)
	defer t.Stop()
	for {
		select {
		case <-sh.stopTickers:
			return
		case <-t.C:
			sh.do(func() {
				sh.view.refreshAges()
				sh.certsTick()
			})
		}
	}
}

// onEvent runs on engine goroutines. It must not block and must not touch widgets: it coalesces
// and hands over to the main goroutine.
func (sh *shell) onEvent(ev engine.Event) {
	switch ev.Type {
	case engine.EventUIState:
		if ev.UIState == nil {
			return
		}
		st := ev.UIState.Clone()
		sh.pendingMu.Lock()
		queued := sh.pendingUI != nil
		if sh.pendingUI == nil || st.Revision >= sh.pendingUI.Revision {
			sh.pendingUI = &st
		}
		sh.pendingMu.Unlock()
		if !queued {
			sh.do(sh.flushUIState)
		}
	case engine.EventTable:
		if sh.tableQueued.CompareAndSwap(false, true) {
			sh.do(func() {
				sh.tableQueued.Store(false)
				sh.refreshTable()
			})
		}
	case engine.EventDetail:
		if sh.detailQueued.CompareAndSwap(false, true) {
			sh.do(func() {
				sh.detailQueued.Store(false)
				sh.drawer.request()
			})
		}
	case engine.EventMetrics:
		if ev.View == engine.ClusterOverview { // the cluster Overview's charts, not a drawer's
			sh.do(func() {
				if sh.ui.View == engine.ClusterOverview {
					sh.view.cluster.request()
				}
			})
			return
		}
		if sh.metricsQueued.CompareAndSwap(false, true) {
			sh.do(func() {
				sh.metricsQueued.Store(false)
				sh.drawer.metricsChanged()
				sh.settings.metricsChanged() // the Prometheus setting may be new
				sh.integrationsChanged("")   // and the metrics integrations' status (clustersettings_integrations.go)
			})
		}
	case engine.EventIntegrations: // what Cogpit knows of a cluster's integrations (clustersettings_integrations.go, D-76)
		ctx := ev.View
		sh.do(func() { sh.integrationsChanged(ctx) })
	case engine.EventAlerts: // the shown cluster's Alertmanager answered something new (alertboard.go, D-77)
		contextID := ev.View
		sh.do(func() {
			if sh.ui.Context == contextID {
				sh.view.alerts.request()
			}
			sh.settings.alertmanagerChanged() // its address's line (clustersettings_alertmanager.go)
		})
	case engine.EventNamespaces: // a namespace the filter may name came, went or began terminating (nsgone.go, AW11)
		ctx := ev.View
		sh.do(func() { sh.namespacesChanged(ctx) })
	case engine.EventDiagnostics:
		sh.requestDiagnostics()
	case engine.EventKinds:
		// A custom resource definition changed: reload the kinds for the sidebar and the View menu.
		if sh.kindsQueued.CompareAndSwap(false, true) {
			sh.do(func() {
				sh.kindsQueued.Store(false)
				if sh.ui.Connection.State.Live() {
					sh.loadKinds()
				}
			})
		}
	case engine.EventToast:
		level, msg, id := ev.Level, ev.Message, ev.Notification
		sh.do(func() { sh.showToast(level, msg, id) }) // the engine keeps it in its notification log
	case engine.EventDrains:
		sh.drains.request()
	case engine.EventHelm:
		sh.helm.request()
	case engine.EventHelmRepos:
		sh.helmHTTP.request() // a repository waiting to send credentials over http (H-40)
	case engine.EventDrafts:
		sh.drafts.request()
	case engine.EventEditor:
		id, rev := ev.View, ev.Revision
		sh.do(func() { sh.dock.editorEvent(id, rev) })
	case engine.EventContexts:
		sh.do(func() {
			sh.kc.contextsChanged()
			sh.hot.request() // a tile's context may have gone, or come back
		})
	case engine.EventAudit: // a line in the audit log: the Audit Log tab reads it (auditlog.go)
		sh.do(sh.dock.auditChanged)
	case engine.EventConnectLog: // a context's connection log changed: its open dialog reads it (connectlog.go)
		id := ev.View
		sh.do(func() { sh.kc.connectLogChanged(id) })
	case engine.EventDockRead: // an agent read a terminal tab: it flashes (dockagentread.go)
		id := ev.View
		sh.do(func() { sh.dock.flashAgentRead(id) })
	case engine.EventChat: // the agent chat: a conversation or the lists changed (chat.go)
		sh.chat.event(ev)
	}
}

// flushUIState applies the newest queued UI state.
func (sh *shell) flushUIState() {
	sh.pendingMu.Lock()
	st := sh.pendingUI
	sh.pendingUI = nil
	sh.pendingMu.Unlock()
	if st != nil {
		sh.applyUIState(*st)
	}
}

// requestDiagnostics reads the counters off the main goroutine, at most every diagnosticsEvery,
// and shows them. It may be called from any goroutine.
func (sh *shell) requestDiagnostics() {
	if !sh.diagQueued.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer sh.recoverTo("diagnostics")
		wait := time.Duration(sh.lastDiagFetch.Load()+int64(diagnosticsEvery)) - time.Duration(time.Now().UnixNano())
		if wait > 0 {
			select {
			case <-time.After(wait):
			case <-sh.ctx.Done():
				return
			}
		}
		sh.lastDiagFetch.Store(time.Now().UnixNano())
		sh.diagQueued.Store(false)
		dv := newDiagView(sh.eng.Diagnostics())
		sh.do(func() {
			sh.diag = dv
			sh.status.update()
			sh.dock.diagnosticsChanged() // an open Diagnostics tab (diagnostics.go)
		})
	}()
}

// applyUIState makes the widgets show st. Revisions only move forward: an older state that
// arrives late from another engine goroutine is dropped.
func (sh *shell) applyUIState(st engine.UIState) {
	if sh.uiApplied && st.Revision < sh.ui.Revision {
		return
	}
	old := sh.ui
	first := !sh.uiApplied
	sh.ui, sh.uiApplied = st, true

	if first || st.Context != old.Context || st.Connection != old.Connection {
		sh.connectionChanged(st, old)
	}
	sh.clustersChanged(st, old, first) // the other connected clusters' marks (clustermarks.go, D-69)
	sh.side.userChanged(st, old)       // a renewed login's user (clusteruser.go, D-68)
	if !first && (st.ActiveTab != old.ActiveTab || st.View != old.View) {
		sh.view.dropSearch() // a search typed for one tab or view stays out of the next (tableview.go)
	}
	if first || st.View != old.View {
		sh.side.selectView(st.View)
		sh.view.viewChanged()
		sh.refreshTable()
	}
	if first || tableStateChanged(st, old) {
		sh.view.stateChanged()
	}
	if first || !slices.Equal(st.Selected, old.Selected) {
		sh.view.selectionChanged(st.Selected)
	}
	if first || !sameRef(st.Drawer, old.Drawer) || st.View != old.View {
		sh.view.drawerChanged(st.Drawer) // its row's highlight and the scroll to it (tableview.go)
	}
	if first || !sameRef(st.Drawer, old.Drawer) {
		sh.drawerChanged()
	}
	if first || !dockEqual(st, old) {
		sh.dock.sync(st)
		sh.layoutBody()
	}
	if first || !sameHotbar(st.Hotbar, old.Hotbar) || st.Context != old.Context || st.Connection != old.Connection {
		sh.hot.stateChanged()
	}
	if !first && st.Overview != old.Overview && st.View == engine.ClusterOverview {
		sh.view.cluster.showControls() // the charts' range or node group, whoever set it
		sh.view.cluster.request()
	}
	if first || st.Palette || old.Palette { // while open, what it lists follows the state
		sh.palette.stateChanged()
	}
	if first || !reflect.DeepEqual(st.Preferences, old.Preferences) {
		sh.preferencesChanged()
	}
	if !first && (!maps.EqualFunc(st.ClusterSettings, old.ClusterSettings, engine.ClusterSettings.Equal) || st.Context != old.Context) {
		sh.identitiesChanged()
	}
	if first || settingsShowChanged(st, old) {
		sh.settings.stateChanged()
	}
	if first || integrationsQuestionChanged(st, old) {
		sh.intq.stateChanged() // the question of a new cluster's integrations (integrationsask.go, D-76, D-82)
	}
	if first || (st.Context == "") != (old.Context == "") || st.Connection != old.Connection || layoutMenuChanged(st, old) {
		// Cluster Settings… is enabled while a cluster is chosen, Disconnect while one is connected or
		// connecting (a reconnect too: menus_cluster.go), Refresh Access Reviews while one is connected
		// (menuaccess.go), the View menu's kinds while connected (loadKinds rebuilds it once they come).
		sh.setMainMenu()
	}
	if first || st.Scope() != old.Scope() {
		sh.side.scopeChanged() // the section's clusters or the cluster's tree (navigator_sections.go)
	}
	if first || !reflect.DeepEqual(st.Navigator, old.Navigator) {
		sh.side.navigatorChanged(old.Navigator) // its search, keyboard row and collapsed groups (navigatorstate.go)
	}
	if first || tabsChanged(st, old) {
		sh.tabs.sync()
	}
	if first || helmChartsShown(st) != helmChartsShown(old) {
		sh.helmHTTP.request() // shown with the Charts view and Helm Install tabs (helmhttp.go)
	}
	sh.chat.stateChanged(old, first) // the chat column: shown, its cluster, the input's state (chat.go)
	sh.layoutChanged(old, first)     // the navigator, the dock's open state, the layout toggles (layoutbar.go)
	sh.top.stateChanged()            // the history arrows and the bell (titlehistory.go)
	sh.dock.takeFocus()              // a tab the window asked the keys for, once shown (dockfocus.go)
}

// connectionChanged follows the context or the connection: the header, the status bar, the
// placeholder, and the connected cluster's kinds and namespaces.
func (sh *shell) connectionChanged(st, old engine.UIState) {
	sh.blink.observeAll(clusterStates(st)) // before the dots follow (conndot.go): every cluster's, D-69
	sh.status.update()
	sh.conn.update()            // the banner of an interrupted connection (D-58)
	sh.dock.connectionChanged() // its shells' Reconnect (dockreconnect.go)
	sh.side.updateHeader()
	sh.view.updateTitle() // a view not read before the interruption has no count and does not sync
	sh.view.updatePlaceholder()
	sh.view.checksChanged() // the selection bar's Delete follows the connection (D-58)
	// A connected cluster shown again loads its kinds, an interrupted one too (D-69).
	if st.Connection.State.Live() && (sh.kindsFor != st.Context) {
		sh.forgetNamespaces()
		sh.loadKinds()
		sh.loadNamespaces()
	}
	if !st.Connection.State.Live() && old.Connection.State.Live() { // an interruption keeps them (D-58)
		sh.kinds, sh.kindsFor, sh.namespaces, sh.nsFor, sh.nsTerminating = nil, "", nil, "", nil
		sh.side.setKinds(nil) // the View menu follows in applyUIState, with the connection's other items
	}
}

// settingsShowChanged reports whether the Settings page has to follow the state: the page itself,
// or while it is open the preferences, the login item, the clusters' settings or the connection.
func settingsShowChanged(st, old engine.UIState) bool {
	return !reflect.DeepEqual(st.Settings, old.Settings) || (st.Settings != nil && (!reflect.DeepEqual(st.Preferences, old.Preferences) ||
		!reflect.DeepEqual(st.LoginItem, old.LoginItem) ||
		!maps.EqualFunc(st.ClusterSettings, old.ClusterSettings, engine.ClusterSettings.Equal) || st.Context != old.Context || st.Connection != old.Connection))
}

// refreshTable shows the newest snapshot of the open view.
func (sh *shell) refreshTable() {
	if sh.ui.View == "" {
		sh.view.setSnapshot(nil)
		return
	}
	sh.view.setSnapshot(sh.eng.Table(sh.ui.View))
	if engine.IsPage(sh.ui.View) {
		sh.view.requestPage() // the page's kinds or totals changed with its table
	}
}

// drawerChanged opens, closes or retargets the detail drawer.
func (sh *shell) drawerChanged() {
	if sh.ui.Drawer == nil {
		sh.drawer.clear()
		sh.workspace.Objects = []fyne.CanvasObject{sh.view.root}
	} else {
		sh.workspace.Objects = []fyne.CanvasObject{sh.hsplit}
		sh.hsplit.Refresh()
		sh.drawer.request()
	}
	sh.workspace.Refresh()
}

// layoutBody puts the dock below the workspace: a resizable split while the dock is open, only
// its tab strip while it is collapsed.
func (sh *shell) layoutBody() {
	if sh.dock.open() {
		sh.vsplit.Leading, sh.vsplit.Trailing = sh.workArea, sh.dock.panel
		sh.dock.attachStrip(true)
		sh.body.Objects = []fyne.CanvasObject{sh.vsplit}
		sh.vsplit.Refresh()
	} else {
		sh.dock.attachStrip(false)
		sh.body.Objects = []fyne.CanvasObject{container.NewBorder(nil, sh.dock.collapsedBar, nil, nil, sh.workArea)}
	}
	sh.body.Refresh()
}

// loadKinds fetches the connected cluster's kinds for the sidebar and the View menu.
func (sh *shell) loadKinds() {
	ctxName := sh.ui.Context
	sh.kindsFor = ctxName
	sh.kindsSeq++
	seq := sh.kindsSeq
	sh.dispatch("kinds.list", struct{}{}, func(res any, err error) {
		if sh.ui.Context != ctxName || seq != sh.kindsSeq {
			return
		}
		if err != nil {
			sh.toast("error", "Could not list the cluster's kinds: "+err.Error())
			return
		}
		kinds, derr := decodeKinds(res)
		if derr != nil {
			sh.toast("error", "Unexpected kinds.list result: "+derr.Error())
			return
		}
		sh.kinds = kinds
		sh.side.setKinds(kinds)
		sh.side.selectView(sh.ui.View)
		sh.view.updateTitle()
		sh.view.updateNamespaceButton()
		sh.view.create.update() // local and virtual kinds have no "+"
		sh.setMainMenu()
	})
}

// loadNamespaces reads the namespace names from the cache for the namespace filter and the fields
// that offer namespaces (followNamespaces). It runs when a cluster is shown and again whenever the
// filter or such a field opens, so that a namespace created since is offered (the owner's walk,
// 2026-10-17, WF10: walk-test, created in the window, was missing from both until the cluster was
// shown anew), and while the filter names namespaces when the view's rows change (nsgone.go); it
// reads the cache, never the API server, every namespace page by page (nsPageSize), and notes the
// terminating ones. Only a changed list touches the widgets.
func (sh *shell) loadNamespaces() {
	ctxName := sh.ui.Context
	var names []string
	terminating := map[string]bool{}
	var page func(offset int)
	page = func(offset int) {
		sh.dispatch("resources.list", listParams{Kind: kindNamespaces, Limit: nsPageSize, Offset: offset}, func(res any, err error) {
			if err != nil || sh.ui.Context != ctxName {
				return // the filter then offers "All namespaces" only; nothing to tell the user
			}
			rows, total, derr := decodeNamespacePage(res)
			if derr != nil {
				return
			}
			for _, r := range rows {
				names = append(names, r.Ref.Name)
				if r.Status == model.StatusTerminating {
					terminating[r.Ref.Name] = true
				}
			}
			if next := offset + len(rows); len(rows) > 0 && next < total {
				page(next)
				return
			}
			slices.Sort(names)
			names = slices.Compact(names)
			sh.namespacesRead(ctxName, names, terminating)
		})
	}
	page(0)
}

// namespacesRead takes in the namespaces read for a context: the filter's list and button and the
// fields that offer them follow a change.
func (sh *shell) namespacesRead(ctxName string, names []string, terminating map[string]bool) {
	same := slices.Equal(names, sh.namespaces) && sh.namespaces != nil
	if same && sh.nsFor == ctxName && maps.Equal(terminating, sh.nsTerminating) {
		return
	}
	sh.namespaces, sh.nsFor, sh.nsTerminating = names, ctxName, terminating
	sh.view.namespacesLoaded()
	if same {
		return
	}
	for _, id := range slices.Sorted(maps.Keys(sh.nsFollow)) {
		sh.nsFollow[id](slices.Clone(names))
	}
}

// forgetNamespaces drops the namespaces read for the cluster shown before another one, so that the
// filter and the fields that offer namespaces never offer that cluster's: loadNamespaces keeps the
// last list when its read fails, and the read of a cluster whose login expired or whose connection
// was interrupted before its namespaces were read waits until the cluster is back (the owner's
// finding of 2026-10-22: acme-p-pinniped, its login expired, offered acme-main's namespaces).
func (sh *shell) forgetNamespaces() {
	if sh.namespaces == nil && sh.nsFor == "" {
		return
	}
	sh.namespaces, sh.nsFor, sh.nsTerminating = nil, "", nil
	sh.view.namespacesLoaded()
	for _, id := range slices.Sorted(maps.Keys(sh.nsFollow)) {
		sh.nsFollow[id](nil)
	}
}

// refreshNamespaces reads the namespace names again when the shown cluster lets the user list them.
func (sh *shell) refreshNamespaces() {
	if live := sh.ui.Connection.State.Live(); live && sh.namespacesListable() {
		sh.loadNamespaces()
	}
}

// followNamespaces calls fn on the main goroutine with the namespace names each time they change,
// until stop is called; an open dialog's namespace fields follow them so.
func (sh *shell) followNamespaces(fn func([]string)) (stop func()) {
	if sh.nsFollow == nil {
		sh.nsFollow = map[int]func([]string){}
	}
	sh.nsFollowID++
	id := sh.nsFollowID
	sh.nsFollow[id] = fn
	return func() { delete(sh.nsFollow, id) }
}

// kindTitle returns the display title of a kind key (from kinds.list, else the key itself) or of
// a page.
func (sh *shell) kindTitle(key string) string {
	if t, ok := engine.PageTitle(key); ok {
		return t
	}
	for _, k := range sh.kinds {
		if k.Kind == key {
			return k.Title
		}
	}
	if key == "" {
		return ""
	}
	return strings.ToUpper(key[:1]) + key[1:]
}

// dispatch runs a command off the main goroutine, then calls done with its result on the main
// goroutine. Without done, an error is shown as a toast; a declined confirmation is not an error.
func (sh *shell) dispatch(id string, params any, done func(any, error)) {
	raw, err := json.Marshal(params)
	if err != nil {
		sh.toast("error", fmt.Sprintf("%s: %v", id, err))
		return
	}
	go sh.run(id, raw, done)
}

// dispatchInOrder dispatches as dispatch does, once every earlier dispatchInOrder has finished: a
// setting changed twice in quick succession is written in the order it was changed, where two
// goroutines could write the older value last (CI, 2026-10-16: TestHideMetricsFromTheSettings).
func (sh *shell) dispatchInOrder(id string, params any, done func(any, error)) {
	raw, err := json.Marshal(params)
	if err != nil {
		sh.toast("error", fmt.Sprintf("%s: %v", id, err))
		return
	}
	sh.orderMu.Lock()
	prev, mine := sh.orderTail, make(chan struct{})
	sh.orderTail = mine
	sh.orderMu.Unlock()
	go func() {
		defer close(mine)
		if prev != nil {
			select {
			case <-prev:
			case <-sh.ctx.Done():
				return
			}
		}
		sh.run(id, raw, done)
	}()
}

// run dispatches one command from the GUI and hands its result to done on the main goroutine, or
// shows its error.
func (sh *shell) run(id string, raw json.RawMessage, done func(any, error)) {
	defer sh.recoverTo(id)
	res, err := sh.eng.Dispatch(sh.ctx, engine.Invocation{ID: id, Params: raw, Origin: engine.OriginGUI})
	if err == nil {
		sh.focusOpened(res) // a tab the window's own command opened or showed takes the keys (dockfocus.go)
	}
	if done == nil && (err == nil || errors.Is(err, engine.ErrDeclined) || sh.ctx.Err() != nil) {
		return
	}
	sh.do(func() {
		if done != nil {
			done(res, err)
			return
		}
		sh.showError(err)
	})
}

// showError shows a dispatch error as a toast; a declined confirmation is the user's answer, and a
// connect that did not fail is none (engine.ConnectNotFailed: the user's Cancel Connecting, a
// connect that goes on in the background, one superseded before it began; the owner's AW15 and
// AW16 of 2026-10-22): the cluster's dot and its connection log say what happened.
func (sh *shell) showError(err error) {
	if err == nil || errors.Is(err, engine.ErrDeclined) || engine.ConnectNotFailed(err) {
		return
	}
	var dis *engine.DisabledError
	if errors.As(err, &dis) {
		sh.toast("warning", dis.Reason)
		return
	}
	sh.toast("error", err.Error())
}

// recoverTo turns a panic in a shell goroutine into a toast instead of ending the process (D-11).
func (sh *shell) recoverTo(what string) {
	if r := recover(); r != nil {
		msg := fmt.Sprintf("internal error in %s: %v", what, r)
		sh.do(func() { sh.toast("error", msg) })
	}
}

// commandEnabled evaluates a command's Enabled for params: nil when it can run, an error whose
// text is the reason otherwise. Call it off the main goroutine.
func (sh *shell) commandEnabled(id string, params any) (err error) {
	cmd, ok := sh.eng.Registry().Get(id)
	if !ok {
		return errUnavailable
	}
	raw, err := json.Marshal(params)
	if err != nil {
		return err
	}
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("unavailable: %v", r)
		}
	}()
	return cmd.Enabled(sh.sess, raw)
}

// errUnavailable marks a command this build does not register; its button is hidden.
var errUnavailable = errors.New("not available in this build")

// toastLifetimes is how long a toast stays per level; an error stays longest, because it asks for
// a reaction. The pointer over a toast pauses its time.
var toastLifetimes = map[string]time.Duration{
	engine.LevelInfo: 6 * time.Second, engine.LevelWarning: 9 * time.Second, engine.LevelError: 14 * time.Second,
}

// toast shows a message the window raised itself and keeps it in the engine's notification log
// (notifications.go), for the window's eyes only.
func (sh *shell) toast(level, msg string) {
	if msg == "" {
		return
	}
	sh.showToast(level, msg, sh.recordToast(level, msg))
}

// copyToast shows a copy's confirmation as a toast only: copying is not worth a place in the
// notification history (the owner's F5, 2026-10-15), so nothing is recorded and the bell does not
// count it.
func (sh *shell) copyToast(level, msg string) { sh.showToast(level, msg, 0) }

// showToast shows a message in the bottom-right corner: a card on a strong surface with a bar,
// border and icon in the level's colour, a bold title, the text and a close button, which marks
// its notification (id, 0 for none) read.
func (sh *shell) showToast(level, msg string, id uint64) {
	if msg == "" {
		return
	}
	c, title, icon := toastStyle(level)
	life := toastLifetimes[level]
	if life == 0 {
		life = toastLifetimes[engine.LevelInfo]
	}
	if sh.toastTime > 0 {
		life = sh.toastTime
	}
	th := sh.app.Settings().Theme()
	pad := th.Size(theme.SizeNamePadding)
	card := &toastCard{}
	card.ExtendBaseWidget(card)
	// The text is selectable, and a copy icon before the × copies the whole message (D-99,
	// toastcopy.go).
	copyIcon := sh.newToastCopy(card, msg)
	text := sh.newToastText(card, msg,
		th.Size(sizeToastWidth)-2*th.Size(theme.SizeNameInlineIcon)-8*pad-toastBar-copyIcon.MinSize().Width-pad)
	head := newTextLine(title, colorToastText, theme.SizeNameText, fyne.TextStyle{Bold: true})
	bg := newSurface(colorToast)
	bg.stroke = c
	bar := newSurface(c)
	bar.min = fyne.NewSize(toastBar, 0)
	closeBtn := newToastClose(card, func() {
		sh.dismissToast(card)
		sh.markToastRead(id)
	})
	card.life = life
	card.expire = func() { sh.do(func() { sh.dismissToast(card) }) }
	card.content = container.NewStack(
		bg,
		container.NewBorder(nil, nil, bar, nil,
			inset(theme.SizeNamePadding, theme.SizeNamePadding,
				container.NewBorder(nil, nil,
					container.NewVBox(container.NewPadded(widget.NewIcon(themedIcon(icon, c)))),
					container.NewVBox(container.NewHBox(container.NewCenter(copyIcon), closeBtn)),
					container.NewVBox(head, text)))),
	)
	sh.toasts.Objects = append(sh.toasts.Objects, card)
	sh.toasts.Refresh()
	card.arm()
}

// toastBar is the width of the level's colour bar at a toast's left edge.
const toastBar = 5

// dismissToast removes a toast, on expiry or by its close button.
func (sh *shell) dismissToast(card *toastCard) {
	card.disarm()
	card.dropFocus(sh.win)
	sh.toasts.Objects = slices.DeleteFunc(sh.toasts.Objects, func(o fyne.CanvasObject) bool { return o == card })
	sh.toasts.Refresh()
}

// toastCard is one toast. Its time runs while the pointer is away: entering stops it, leaving
// starts the full lifetime again. Its text selected, or being selected, holds it too (toastcopy.go).
type toastCard struct {
	widget.BaseWidget
	content   fyne.CanvasObject
	life      time.Duration
	expire    func() // called on a timer goroutine
	mu        sync.Mutex
	timer     *time.Timer
	hovered   bool
	selecting bool
	text      *selParagraph
}

var _ desktop.Hoverable = (*toastCard)(nil)

func (c *toastCard) arm() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.timer != nil {
		c.timer.Stop()
	}
	if !c.hovered && !c.selecting {
		c.timer = time.AfterFunc(c.life, c.expire)
	}
}

func (c *toastCard) disarm() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.timer != nil {
		c.timer.Stop()
		c.timer = nil
	}
}

// MouseIn implements desktop.Hoverable.
func (c *toastCard) MouseIn(*desktop.MouseEvent) {
	c.mu.Lock()
	c.hovered = true
	c.mu.Unlock()
	c.disarm()
}

// MouseMoved implements desktop.Hoverable.
func (c *toastCard) MouseMoved(*desktop.MouseEvent) {}

// MouseOut implements desktop.Hoverable.
func (c *toastCard) MouseOut() {
	c.mu.Lock()
	c.hovered = false
	c.mu.Unlock()
	c.arm()
}

// CreateRenderer implements fyne.Widget.
func (c *toastCard) CreateRenderer() fyne.WidgetRenderer { return widget.NewSimpleRenderer(c.content) }

// toastLayout stacks toasts upwards from the bottom-right corner, above the status bar.
type toastLayout struct{}

func (toastLayout) MinSize([]fyne.CanvasObject) fyne.Size { return fyne.Size{} }

func (toastLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	th := fyne.CurrentApp().Settings().Theme()
	pad := th.Size(theme.SizeNamePadding) * 3
	w := th.Size(sizeToastWidth)
	y := size.Height - th.Size(sizeStatusHeight) - pad
	for i := len(objs) - 1; i >= 0; i-- {
		o := objs[i]
		h := o.MinSize().Height
		y -= h
		o.Move(fyne.NewPos(size.Width-w-pad, y))
		o.Resize(fyne.NewSize(w, h))
		y -= pad / 2
	}
}

// mainMenu builds the window menu: Cluster, View (one item per kind), Helm, on Windows and Linux
// Window, and Help (menus.go).
func (sh *shell) mainMenu() *fyne.MainMenu {
	cluster := fyne.NewMenu("Cluster", sh.connectMenuItem())
	if item := sh.hot.contextItem(sh.ui.Context); item != nil { // Lens's cluster menu: "Add to Hotbar"
		cluster.Items = append(cluster.Items, item)
	}
	cluster.Items = append(cluster.Items, sh.clusterMenuItems()...) // Disconnect, Remove from Cogpit… (menus_cluster.go)
	cluster.Items = append(cluster.Items, sh.kc.menuItems()...)
	cluster.Items = append(cluster.Items, sh.refreshAccessItem())
	cluster.Items = append(cluster.Items, sh.settingsMenuItems()...) // ⌘⇧, and ⌘, from any focus
	var items []*fyne.MenuItem
	for _, k := range listableKinds(sh.kinds) { // as the sidebar: no kind the user may not list
		key := k.Kind
		items = append(items, fyne.NewMenuItem(k.Title, func() {
			sh.dispatch("ui.openView", openViewParams{View: key}, nil)
		}))
	}
	if len(items) == 0 {
		none := fyne.NewMenuItem(notConnected, nil)
		none.Disabled = true
		items = append(items, none)
	}
	view := fyne.NewMenu("View", items...)
	// The tab bar's ⌘W, ⌘⌥→ and ⌘⌥←, ⌘1–9, ⌃- and ⌃⇧-, from any focus (tabbar.go).
	view.Items = append(sh.tabs.menuItems(), view.Items...)
	if keys := sh.windowKeyItems(); len(keys) > 0 { // ⌘F and ⌘⇧T shown, the keys kept by the window (A55, menukeys.go)
		view.Items = append(append(keys, fyne.NewMenuItemSeparator()), view.Items...)
	}
	// ⌘B, ⌘J (layoutbar.go), ⌘I and ⌘. (chat.go), from any focus.
	if chat := append(sh.layoutMenuItems(), sh.chatMenuItems()...); len(chat) > 0 {
		view.Items = append(append(chat, fyne.NewMenuItemSeparator()), view.Items...)
	}
	if pal := sh.paletteMenuItem(); pal != nil { // ⌘⇧P, from any focus: menu shortcuts come first
		view.Items = append([]*fyne.MenuItem{pal, fyne.NewMenuItemSeparator()}, view.Items...)
	}
	menus := []*fyne.Menu{cluster, view, sh.helmMenu()}
	if window := sh.windowMenu(runtime.GOOS); window != nil { // macOS keeps GLFW's Window menu (menus.go)
		menus = append(menus, window)
	}
	return fyne.NewMainMenu(append(menus, sh.helpMenu())...)
}

// installShortcuts adds Lens's keyboard shortcuts that Cogpit supports, as the registry declares
// them: ⌘F (ui.setFilter: the search) and ⌘⇧T (terminal.open), the window's own (windowKeys: a
// focused widget that takes shortcuts keeps them; the View menu shows them, A55), Escape (the
// innermost thing open: the top overlay, a settings page, the drawer; escape.go), "/" (the logs tab's search), and F1–F12 for
// the hotbar while no text field has the focus (the table and the search fields pass them on, A64).
// ⌘⇧P and ⌘P are the main menu's (mainMenu), which take them from any focus.
func (sh *shell) installShortcuts() {
	c := sh.win.Canvas()
	for _, k := range windowKeys {
		if sc := sh.registryShortcut(k.id); sc != nil {
			run := k.run
			c.AddShortcut(sc, func(fyne.Shortcut) { run(sh) })
		}
	}
	c.SetOnTypedKey(func(ev *fyne.KeyEvent) {
		if sh.hot.key(ev.Name, shiftHeld()) { // F1–F12: the hotbar (Fyne has no shortcut for them)
			return
		}
		if ev.Name == fyne.KeyEscape {
			sh.escape() // the innermost thing open: an overlay, a settings page, the drawer (escape.go)
		}
	})
	c.SetOnTypedRune(func(r rune) {
		if r == '/' {
			sh.dock.focusLogSearch() // "/" focuses the active logs tab's search
		}
	})
}

// closeDrawer asks the engine to close the drawer.
func (sh *shell) closeDrawer() {
	if sh.ui.Drawer != nil {
		sh.dispatch("ui.closeDrawer", struct{}{}, nil)
	}
}

// Command parameters, mirroring the engine's command schemas. Slices are never nil: a JSON null
// would not validate against an array schema.
type (
	openViewParams struct {
		View   string   `json:"view"`
		States []string `json:"states,omitempty"` // opens the view filtered to these states (a dashboard's state link)
	}
	namespacesParams struct {
		Namespaces []string `json:"namespaces"`
	}
	groupsParams struct {
		Groups []string `json:"groups"`
	}
	columnsParams struct {
		View   string   `json:"view"`
		Hidden []string `json:"hidden"`
	}
	filterParams struct {
		Filter    string `json:"filter"`
		MatchCase *bool  `json:"matchCase,omitempty"` // the "Aa" toggle; nil: unchanged
		Regex     *bool  `json:"regex,omitempty"`     // the ".*" toggle; nil: unchanged
	}
	sortParams struct {
		Column string `json:"column"`
		Desc   bool   `json:"desc"`
	}
	selectParams struct {
		Refs     []model.ObjectRef `json:"refs"`
		NoDrawer bool              `json:"noDrawer"`
	}
	refParams struct {
		Ref model.ObjectRef `json:"ref"`
	}
	containerParams struct {
		Ref       model.ObjectRef `json:"ref"`
		Container string          `json:"container"`
	}
	contextParams struct {
		Context string `json:"context"`
	}
	dockParams struct {
		ID string `json:"id"`
	}
	listParams struct {
		Kind   string `json:"kind"`
		Limit  int    `json:"limit,omitempty"`
		Offset int    `json:"offset,omitempty"`
	}
)

// kindInfo is one entry of kinds.list.
type kindInfo struct {
	Kind       string         `json:"kind"`
	Title      string         `json:"title"`
	APIKind    string         `json:"apiKind"`
	Group      string         `json:"group"`
	Version    string         `json:"version"`
	Namespaced bool           `json:"namespaced"`
	Columns    []model.Column `json:"columns"`
	Listable   *bool          `json:"listable,omitempty"` // nil: the access reviews have not answered
	Local      bool           `json:"local,omitempty"`    // port forwards: Cogpit's own objects
	Virtual    bool           `json:"virtual,omitempty"`  // Helm releases: made from other objects
}

// decode turns a command result into out through JSON: results cross the engine seam as
// JSON-serializable values, whatever Go type the engine used for them.
func decode(res any, out any) error {
	var raw []byte
	switch r := res.(type) {
	case json.RawMessage:
		raw = r
	case []byte:
		raw = r
	default:
		b, err := json.Marshal(res)
		if err != nil {
			return err
		}
		raw = b
	}
	return json.Unmarshal(raw, out)
}

// decodeKinds reads kinds.list: an array, or an object with a "kinds" array.
func decodeKinds(res any) ([]kindInfo, error) {
	var list []kindInfo
	if err := decode(res, &list); err == nil {
		return list, nil
	}
	var wrapped struct {
		Kinds []kindInfo `json:"kinds"`
	}
	if err := decode(res, &wrapped); err != nil {
		return nil, err
	}
	return wrapped.Kinds, nil
}

// formatCount formats n with thousands separators: 10000 → "10,000".
func formatCount(n int64) string {
	s := strconv.FormatInt(n, 10)
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	var b strings.Builder
	if neg {
		b.WriteByte('-')
	}
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	return b.String()
}

func sameRef(a, b *model.ObjectRef) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func dockEqual(a, b engine.UIState) bool {
	if a.ActiveDock != b.ActiveDock || len(a.Dock) != len(b.Dock) || a.Context != b.Context { // the shown cluster's tabs (D-69)
		return false
	}
	for i := range a.Dock {
		x, y := a.Dock[i], b.Dock[i]
		if x.ID != y.ID || x.Title != y.Title || x.Logs != y.Logs || x.Container != y.Container || !sameRef(x.Ref, y.Ref) ||
			x.Audit != y.Audit || // the Audit Log's filter (auditlog.go)
			x.AgentRead != y.AgentRead || x.AgentReadExpired != y.AgentReadExpired || x.AgentReadCapped != y.AgentReadCapped { // a terminal's "Agents may read" (dockoutput.go, dockagentread.go)
			return false
		}
		// A Helm tab's chart, version and digest change in place (a version picked, its chart
		// loaded): the tab's editor updates from them (dock.sync).
		if !reflect.DeepEqual(x.Helm, y.Helm) || !reflect.DeepEqual(x.HelmInstall, y.HelmInstall) {
			return false
		}
		// A terminal's kubectl check and its download change in place (kubectloffer.go).
		if !reflect.DeepEqual(x.Kubectl, y.Kubectl) {
			return false
		}
		// A job's steps and state change in place (jobtab.go).
		if !reflect.DeepEqual(x.Job, y.Job) {
			return false
		}
		// A shell's session the interruption ended, and the new one Reconnect opens (dockreconnect.go).
		if !x.Interrupted.Equal(y.Interrupted) || x.Session != y.Session {
			return false
		}
	}
	return true
}
