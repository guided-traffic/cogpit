package fyneui

import (
	"image/color"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// Default column widths by column id, in Fyne units, for columns whose profile gives no width
// hint. They are data about Kubernetes columns (an age is short, a message is long), so they sit
// next to the table and not in the theme.
var defaultColumnWidths = map[string]float32{
	"name": 220, "namespace": 130, "containers": 84, "cpu": 64, "memory": 88, "restarts": 64,
	"controlledBy": 108, "node": 104, "qos": 80, "age": 56, columnStatus: 172, "conditions": 240,
	"pods": 72, "replicas": 86, "type": 100, "clusterIP": 120, "ports": 140, "externalIP": 120,
	"selector": 200, "labels": 260, columnMessage: 300, "involvedObject": 210, "source": 140,
	"count": 60, "lastSeen": 172, "roles": 120, "version": 104, "taints": 64, "disk": 76,
}

var defaultWidthByType = map[model.ColumnType]float32{
	model.ColumnText: 140, model.ColumnNumber: 84, model.ColumnAge: 64, model.ColumnTime: 172,
}

// tableView is the centre of the window: the view header (title and count, namespace filter,
// status filter, search) above the resource table, or a placeholder while there is nothing to show.
type tableView struct {
	sh *shell

	title, count, syncing *textLine
	csvButton             *widget.Button // beside the count: download the view as CSV (tableheader.go)
	errLine               *textLine
	scopeLine             *textLine // the namespaces a kind is read in one by one, and those denied (clustersettings_namespaces_filter.go)
	nsButton              *widget.Button
	nsTint                *container.ThemeOverride // the button red while a namespace it names is gone or going (nsgone.go)
	nsTintTheme           *nsTintTheme
	nsBox                 *fyne.Container // the namespace filter's slot; hidden on cluster-scoped views
	states                *statusFilter   // the status filter beside it (statusfilter.go)
	search                *searchEntry    // F1–F12 are the hotbar's (A64, searchkeys.go)
	toggles               *searchToggles  // the search's "Aa" and ".*" (searchtoggles.go)
	searchBox             *fyne.Container
	heading, tools        *fyne.Container // the header's left and right parts

	// A page shows its own part between the header and the table: the Workloads Overview its kinds'
	// tiles, the cluster Overview its charts (clusteroverview.go); then the table's header (the
	// events', the issues'), which holds the count and the search.
	overview                   *overviewPanel
	cluster                    *clusterPanel
	flux                       *fluxPanel      // the Flux dashboard's tiles (fluxdashboard.go)
	argo                       *argoPanel      // the Argo CD Dashboard's strip, cards and activity (argocd_dashboard.go)
	alerts                     *alertBoard     // the alert board, over the table while its page is open (alertboard.go, D-77)
	pageTop                    *fyne.Container // the open page's part
	eventsTitle                *textLine
	eventsHeading, eventsTools *fyne.Container
	pageBox                    *fyne.Container
	pageMode                   bool
	pageView                   string // the page the layout is set up for
	table                      *resourceTable
	placeholder                *fyne.Container
	placeholderText            *textLine
	placeholderDetail          *textLine       // while connecting: the login it waits for (D-73)
	placeholderDetailEnd       *textLine       // behind a login Cogpit could not end: that it has no end (H-118)
	detailBox                  *fyne.Container // the detail's lines centred; hidden while it says nothing
	connectButton              *widget.Button
	cancelButton               *widget.Button  // Cancel Connecting while connecting (D-73)
	cancelBox                  *fyne.Container // cancelButton centred; hidden unless connecting
	create                     *stripChip      // the create chip in the dock's strip (createbutton.go)
	root                       fyne.CanvasObject

	snap    *engine.TableSnapshot
	cols    []int  // visible column → index in snap.Columns
	colsKey string // the view and column ids, to notice a column change
	now     time.Time
	tf      model.TimeFormat // absolute times (an event's Last Seen) in the user's zone and format (D-31)

	cells      []*tableCell  // every cell the table has shown, pooled ones included
	headers    []*headerCell // every header cell the table made, for their tooltips (tableheadertip.go)
	hoverRow   int
	hoverCol   int
	kbOn       bool // the table has the keyboard focus (tablekeys.go)
	kbRow      int  // the cell the keyboard is on
	kbCol      int
	hoverCell  *tableCell    // the cell under the pointer or the keyboard highlight
	pointer    fyne.Position // the pointer over the table, in window coordinates (tooltips)
	pointerIn  bool
	icons      map[cellIconKey]fyne.Resource // the cells' icons, one per icon and colour
	menu       *widget.PopUpMenu             // the row menu last opened
	menuModel  *fyne.Menu                    // and its items
	kindSet    map[string]bool               // the kinds links may lead to (serves)
	kindSetSrc *kindInfo
	kindSetLen int
	selected   []model.ObjectRef
	pending    *model.ObjectRef // a selected object to scroll to once a snapshot contains it
	check      tableChecks      // the row checkboxes, the header's and the selection bar (tablecheck.go)
	order      *columnDrag      // a header dragged to another place (columnorder.go)
	copyIcon   *tableCopy       // the copy icon on the cell under the pointer (tablecopy.go)

	userWidths map[string]float32 // widths the user dragged, by view and column id
	setWidths  []float32          // the width the shell last gave each visible column
	fitWidth   float32            // the frame width the columns were last fitted to
	fitting    bool               // set while fitColumns sets widths, which are not the user's

	searchTimer *time.Timer
	filterBusy  int  // filter changes typed but not yet acknowledged by the engine
	syncing2    bool // set while the search entry is updated from engine state

	ns *nsFilter // the namespace filter's list (nsfilter.go)

	colPopup  *widget.PopUp // the column list of the header's ⋮ (tableheader.go)
	colChecks []*widget.Check
	colIDs    []string // the column id of each check
	colView   string   // the view the list was opened for
	colSync   bool
}

func newTableView(sh *shell) *tableView {
	tv := &tableView{sh: sh, hoverRow: -1, hoverCol: -1, userWidths: map[string]float32{}, icons: map[cellIconKey]fyne.Resource{}}
	tv.order = newColumnDrag(tv) // before the table: its header cells take the drag's callbacks
	tv.copyIcon = newTableCopy(tv)

	tv.title = newTextLine("", theme.ColorNameForeground, theme.SizeNameSubHeadingText, fyne.TextStyle{Bold: true})
	tv.count = newTextLine("", colorMuted, theme.SizeNameText, fyne.TextStyle{})
	tv.syncing = newTextLine("", colorAccent, theme.SizeNameCaptionText, fyne.TextStyle{})
	tv.errLine = newTextLine("", colorStatusError, theme.SizeNameCaptionText, fyne.TextStyle{})
	tv.errLine.Hide()
	tv.scopeLine = newTextLine("", colorMuted, theme.SizeNameCaptionText, fyne.TextStyle{})
	tv.scopeLine.truncate = true
	tv.scopeLine.Hide()

	tv.nsButton = widget.NewButtonWithIcon("All namespaces", theme.MenuDropDownIcon(), tv.openNamespaces)
	tv.ns = newNSFilter(tv)
	tv.nsButton.IconPlacement = widget.ButtonIconTrailingText
	tv.nsButton.Alignment = widget.ButtonAlignLeading
	tv.search = newSearchEntry(sh)
	tv.toggles = newSearchToggles(tv) // before anything draws the entry: its renderer takes the switches once
	tv.search.SetPlaceHolder("Search…")
	tv.search.SetIcon(theme.SearchIcon())
	tv.search.OnChanged = tv.searchChanged

	tv.table = newResourceTable(tv)
	tv.search.back = tv.table // Escape in the search clears it and hands the table the keys (AW4)

	tv.placeholderText = newTextLine("", colorMuted, theme.SizeNameText, fyne.TextStyle{})
	tv.placeholderText.align, tv.placeholderText.truncate = fyne.TextAlignCenter, true                            // never beyond the view (AW18)
	tv.connectButton = widget.NewButtonWithIcon(connectLabel+"…", theme.LoginIcon(), func() { sh.findCluster() }) // D-57
	tv.connectButton.Importance = widget.HighImportance
	tv.placeholderDetail = newTextLine("", colorMuted, theme.SizeNameText, fyne.TextStyle{})
	tv.placeholderDetailEnd = newTextLine("", colorMuted, theme.SizeNameText, fyne.TextStyle{})
	tv.placeholderDetail.align, tv.placeholderDetailEnd.align = fyne.TextAlignCenter, fyne.TextAlignCenter
	tv.placeholderDetailEnd.Hide()
	tv.cancelButton = widget.NewButtonWithIcon(cancelConnectingLabel, theme.CancelIcon(), sh.disconnect) // cluster.disconnect (menus_cluster.go)
	// Hidden as boxes: a centring box around a hidden widget would still take its row's padding.
	tv.detailBox = container.NewCenter(container.NewVBox(tv.placeholderDetail, tv.placeholderDetailEnd))
	tv.cancelBox = container.NewCenter(tv.cancelButton)
	tv.detailBox.Hide()
	tv.cancelBox.Hide()
	// A failed connect leaves the window on its cluster (D-97, the owner's WF40) with its error page
	// over the workspace (errorpage.go, D-114), which has Connect Again and its connection log.
	tv.placeholder = box(placeholderLayout{text: tv.placeholderText}, container.NewVBox(
		tv.placeholderText, tv.detailBox, container.NewCenter(tv.connectButton), tv.cancelBox))

	tv.csvButton = widget.NewButtonWithIcon("", theme.DownloadIcon(), tv.exportCSV)
	tv.csvButton.Importance = widget.LowImportance
	tv.csvButton.Hide()
	tv.check.bar = newSelectionBar(tv)
	tv.heading = container.NewHBox(tv.title, tv.count, tv.csvButton, tv.syncing, tv.check.bar.root)
	tv.nsTintTheme = &nsTintTheme{app: sh.app}
	tv.nsTint = container.NewThemeOverride(tv.nsButton, tv.nsTintTheme)
	tv.nsBox = sized(sizeFilterWidth, "", tv.nsTint)
	tv.states = newStatusFilter(tv)
	tv.searchBox = sized(sizeSearchWidth, "", tv.toggles.field())
	// The tools give way to a narrow view, the search first (headerrow.go).
	tv.tools = box(&shrinkRowLayout{floors: map[fyne.CanvasObject]float32{
		tv.nsBox: 120, tv.states.box: 110, tv.searchBox: 170,
	}}, tv.nsBox, tv.states.box, tv.searchBox)
	header := container.NewStack(newSurface(theme.ColorNameBackground),
		inset(theme.SizeNameInnerPadding, theme.SizeNamePadding,
			box(headerRowLayout{}, tv.heading, tv.tools)))

	tv.overview = newOverviewPanel(sh)
	tv.cluster = newClusterPanel(sh)
	tv.flux = newFluxPanel(sh)
	tv.argo = newArgoPanel(sh)
	tv.alerts = newAlertBoard(sh)
	tv.pageTop = container.NewStack(tv.overview.root)
	tv.eventsTitle = newTextLine("Events", theme.ColorNameForeground, theme.SizeNameText, fyne.TextStyle{Bold: true})
	tv.eventsHeading, tv.eventsTools = container.NewHBox(), container.NewHBox()
	tv.pageBox = container.NewVBox(tv.pageTop, newDivider(false),
		container.NewStack(newSurface(theme.ColorNameBackground),
			inset(theme.SizeNameInnerPadding, theme.SizeNamePadding,
				container.NewBorder(nil, nil, tv.eventsHeading, tv.eventsTools))))
	tv.pageBox.Hide()
	frame := box(&stretchLayout{tv: tv}, tv.table)
	tv.create = newCreateButton(sh)
	body := container.NewStack(newSurface(theme.ColorNameBackground), frame, tv.order.layer, tv.copyIcon.layer, tv.placeholder, tv.alerts.root)
	tv.root = container.NewBorder(
		container.NewVBox(header, tv.pageBox, inset(theme.SizeNameInnerPadding, "", tv.errLine), inset(theme.SizeNameInnerPadding, "", tv.scopeLine)),
		nil, nil, nil, body)
	tv.updatePlaceholder()
	return tv
}

// showHeadingCount shows the item count and the CSV button while the view has a snapshot and no
// selection bar: the bar takes their place (tablecheck.go).
func (tv *tableView) showHeadingCount() {
	show := tv.snap != nil && (tv.check.bar == nil || !tv.check.bar.root.Visible())
	csv := show && !tv.alerts.shown    // the alert board's count has no table to download (alertboard.go)
	csv = csv && !tv.unreadWhileAway() // nor has a view not read before the cluster went away
	show = show || tv.alerts.shown
	if csv != tv.csvButton.Visible() {
		if csv {
			tv.csvButton.Show()
		} else {
			tv.csvButton.Hide()
		}
	}
	if show != tv.count.Visible() {
		if show {
			tv.count.Show()
		} else {
			tv.count.Hide()
		}
	}
}

// setPageLayout arranges the header for a page or a kind's view. On a page the header holds the
// page's title and the namespace filter, which applies to the whole page; the page's own part
// follows (the Workloads Overview's tiles, the cluster Overview's charts), then the table's own
// header (Events, Issues) with its count, download button and search.
func (tv *tableView) setPageLayout(view string) {
	// The alert board's header is a view's: its title and count, the namespace filter and the
	// search; its body replaces the table (alertboard.go, D-77).
	board := view == engine.AlertBoard
	tv.alerts.setShown(board)
	if board { // its groups are the severities: no status filter
		tv.states.box.Hide()
	} else {
		tv.states.box.Show()
	}
	page := engine.IsPage(view) && !board
	if page && view != tv.pageView {
		tv.pageView = view
		top, title := tv.overview.root, "Events"
		switch view {
		case engine.ClusterOverview:
			top, title = tv.cluster.root, "Issues"
		case engine.FluxDashboard:
			top, title = tv.flux.root, fluxTableTitle
		}
		if view == engine.ArgoCDDashboard {
			top, title = tv.argo.root, "Needs Attention"
		}
		tv.pageTop.Objects = []fyne.CanvasObject{top}
		tv.pageTop.Refresh()
		tv.eventsTitle.SetText(title)
	}
	if page == tv.pageMode {
		return
	}
	tv.pageMode = page
	if page {
		tv.heading.Objects = []fyne.CanvasObject{tv.title}
		tv.tools.Objects = []fyne.CanvasObject{tv.nsBox}
		tv.eventsHeading.Objects = []fyne.CanvasObject{tv.eventsTitle, tv.count, tv.csvButton, tv.syncing, tv.check.bar.root}
		tv.eventsTools.Objects = []fyne.CanvasObject{tv.states.box, tv.searchBox} // the status filter filters the table only
		tv.pageBox.Show()
	} else {
		tv.eventsHeading.Objects, tv.eventsTools.Objects = nil, nil
		tv.heading.Objects = []fyne.CanvasObject{tv.title, tv.count, tv.csvButton, tv.syncing, tv.check.bar.root}
		tv.tools.Objects = []fyne.CanvasObject{tv.nsBox, tv.states.box, tv.searchBox}
		tv.pageBox.Hide()
	}
	for _, c := range []*fyne.Container{tv.heading, tv.tools, tv.eventsHeading, tv.eventsTools} {
		c.Refresh()
	}
}

// viewChanged follows a change of the open view: titles, placeholder, search hint.
func (tv *tableView) viewChanged() {
	tv.pending = nil
	tv.hoverRow, tv.hoverCol, tv.hoverCell = -1, -1, nil
	tv.sh.tip.hide()
	tv.copyIcon.hide()
	page := engine.IsPage(tv.sh.ui.View)
	tv.setPageLayout(tv.sh.ui.View)
	if page {
		tv.requestPage()
	}
	tv.updateTitle()
	tv.updatePlaceholder()
	tv.updateNamespaceButton()
	tv.states.update()
}

// requestPage asks for the open page's own part: the Workloads Overview's kinds, the cluster
// Overview's charts.
func (tv *tableView) requestPage() {
	switch view := tv.sh.ui.View; {
	case view == engine.ClusterOverview:
		tv.cluster.request()
	case view == engine.FluxDashboard:
		tv.flux.request()
	case view == engine.ArgoCDDashboard:
		tv.argo.request()
	case view == engine.AlertBoard:
		tv.alerts.request()
	case engine.IsPage(view):
		tv.overview.request()
	}
}

// rowNoun names the open view's rows in hints: "pods"; a page's table lists events, the cluster
// Overview's issues ("No issues found.", inferred wording).
func (tv *tableView) rowNoun() string {
	if tv.sh.ui.View == engine.ClusterOverview {
		return "issues"
	}
	if tv.sh.ui.View == engine.FluxDashboard {
		return "Flux objects"
	}
	if tv.sh.ui.View == engine.ArgoCDDashboard {
		return argoRowNoun(tv.sh) // argocd.go
	}
	if tv.sh.ui.View == engine.AlertBoard {
		return "alerts"
	}
	if engine.IsPage(tv.sh.ui.View) {
		return kindEvents // the kind key reads as the noun
	}
	return strings.ToLower(tv.sh.kindTitle(tv.sh.ui.View))
}

// updateTitle shows the view's title and item count, "Pods 18 items", and names the view in the
// search hint; kinds.list may bring the title after the view opened.
func (tv *tableView) updateTitle() {
	tv.search.SetPlaceHolder("Search " + tv.rowNoun() + "…")
	title := tv.sh.kindTitle(tv.sh.ui.View)
	if tv.snap != nil && tv.snap.Title != "" {
		title = tv.snap.Title
	}
	tv.title.SetText(title)
	if tv.snap == nil {
		tv.count.SetText(tv.alerts.countText()) // "" unless the alert board shows (alertboard.go)
		tv.syncing.SetText("")
		tv.showHeadingCount()
		return
	}
	switch {
	case tv.unreadWhileAway(): // no count of what was never read, and nothing syncs
		tv.count.SetText("")
		tv.syncing.SetText("")
	case tv.snap.Synced:
		tv.count.SetText(itemCount(len(tv.snap.Rows), tv.snap.Total))
		tv.syncing.SetText("")
	default:
		tv.count.SetText(itemCount(len(tv.snap.Rows), tv.snap.Total))
		tv.syncing.SetText("syncing…")
	}
	tv.showHeadingCount()
}

// unreadWhileAway reports whether the open view shows nothing because its objects were not read
// before the connection was interrupted or the login expired (a view opened since, a cluster shown
// again after its cache went cold): the cache's lists wait until the cluster is back
// (cluster.HealthInterrupted, HealthLoginExpired), so nothing loads or syncs meanwhile and the
// view says so instead of "Loading…" and "syncing…" (the owner's finding of 2026-10-22). A view
// read before keeps its rows as of then (D-58).
func (tv *tableView) unreadWhileAway() bool {
	if st := tv.sh.ui.Connection.State; st != engine.Interrupted && st != engine.LoginExpired {
		return false
	}
	return tv.snap == nil || (!tv.snap.Synced && len(tv.snap.Rows) == 0 && tv.snap.Error == "")
}

// unreadText is the placeholder of a view unreadWhileAway: why it is empty and when it loads.
func unreadText(noun string, st engine.ConnectionState) string {
	if st == engine.LoginExpired {
		return "The " + noun + " were not read before the login expired; they load once it is renewed."
	}
	return "The " + noun + " were not read before the cluster stopped answering; they load once it answers again."
}

// updateConnecting shows, while the cluster connects, the login its credential plugin waits for and
// Cancel Connecting (D-73; the owner's finding of 2026-10-16 night: a login nobody finished could
// not be ended).
func (tv *tableView) updateConnecting(ui engine.UIState) {
	if ui.Connection.State != engine.Connecting {
		tv.detailBox.Hide()
		tv.cancelBox.Hide()
		return
	}
	tv.cancelBox.Show()
	if w := ui.Connection.WaitingForLogin; w.Program != "" {
		what, end := loginWaitSentences(w, tv.sh.timeFormat())
		if w.RunsOn { // two lines, and the button below stops the wait only, not that run (H-118)
			tv.placeholderDetail.SetText(what)
			tv.placeholderDetailEnd.SetText(end + " " + loginWaitCancel(w, ""))
			tv.placeholderDetailEnd.Show()
		} else {
			tv.placeholderDetail.SetText(what + " " + end)
			tv.placeholderDetailEnd.Hide()
		}
		tv.detailBox.Show()
	} else {
		tv.detailBox.Hide()
	}
}

// updatePlaceholder shows the placeholder instead of the table when there is nothing to show.
func (tv *tableView) updatePlaceholder() {
	ui := tv.sh.ui
	msg := ""
	connect := false
	switch {
	case ui.Connection.State == engine.Connecting:
		msg = "Connecting to " + ui.Context + "…"
	case !ui.Connection.State.Live(): // an interrupted cluster keeps its view (D-58)
		msg, connect = notConnected+".", ui.Connection.State != engine.ConnectError || ui.Context == ""
		// A failed connect's error and its actions are its error page's, over the view (D-114); the
		// view says no more than that it is not connected (its long line drew over the navigator, AW18).
		if ui.Connection.State == engine.Disconnected && ui.Connection.Error != "" {
			msg = ui.Connection.Error // Cogpit closed the connection: its kubeconfig file went away
		}
	case ui.View == "":
		msg = "Choose a view in the sidebar."
	case tv.unreadWhileAway():
		msg = unreadText(tv.rowNoun(), ui.Connection.State)
	case tv.snap == nil || (!tv.snap.Synced && len(tv.snap.Rows) == 0 && tv.snap.Error == ""):
		msg = "Loading " + tv.rowNoun() + "…"
	case len(tv.snap.Rows) == 0 && (tv.snap.Filter != "" || len(tv.snap.Namespaces) > 0 || len(tv.snap.States) > 0 || (tv.groupMode() && len(ui.Groups) > 0)):
		msg = "No " + tv.rowNoun() + " match the filter."
	case len(tv.snap.Rows) == 0 && ui.View == engine.FluxDashboard && tv.snap.Error == "":
		msg = "Every Flux object is ready." // its table lists those that are not (fluxdashboard.go)
	case len(tv.snap.Rows) == 0:
		msg = "No " + tv.rowNoun() + " found."
	}
	tv.placeholderText.SetText(msg)
	if connect {
		tv.connectButton.Show()
	} else {
		tv.connectButton.Hide()
	}
	tv.updateConnecting(ui)
	if msg == "" {
		tv.placeholder.Hide()
	} else {
		tv.placeholder.Show()
	}
	tv.create.update()
}

// setSnapshot shows a new snapshot. A replaced snapshot costs one table refresh: the table asks
// for the visible cells only, and each cell update is O(1).
func (tv *tableView) setSnapshot(snap *engine.TableSnapshot) {
	if snap == tv.snap && snap != nil {
		return
	}
	newView := snap != nil && (tv.snap == nil || tv.snap.View != snap.View)
	fewer := snap != nil && tv.snap != nil && len(snap.Rows) < len(tv.snap.Rows)
	tv.snap = snap
	key := ""
	if snap != nil {
		var b strings.Builder
		b.WriteString(snap.View)
		for _, i := range snap.DisplayOrder() { // the order the user arranged (columnorder.go)
			c := snap.Columns[i]
			b.WriteByte('|')
			b.WriteString(c.ID)
			if c.Hidden {
				b.WriteByte('-')
			}
		}
		key = b.String()
	}
	if key != tv.colsKey {
		tv.colsKey = key
		tv.cols = tv.cols[:0]
		if snap != nil {
			for _, i := range snap.DisplayOrder() {
				if !snap.Columns[i].Hidden {
					tv.cols = append(tv.cols, i)
				}
			}
		}
		tv.setWidths = nil
		tv.fitWidth = 0
		tv.fitColumns(tv.table.Size().Width)
	}
	if snap != nil && snap.Error != "" {
		tv.errLine.SetText(snap.Error)
		tv.errLine.Show()
	} else {
		tv.errLine.Hide()
	}
	tv.showScope(snap) // clustersettings_namespaces_filter.go
	tv.now, tv.tf = tv.sh.now(), tv.sh.timeFormat()
	tv.updateTitle()
	tv.updatePlaceholder()
	tv.updateNamespaceButton() // cluster-scoped views hide the namespace filter
	tv.states.update()         // the states the rows carry (statusfilter.go)
	tv.syncColumnChecks()
	tv.checksChanged() // the header's checkbox counts the rows shown
	if newView {
		tv.table.resetKeyboard() // a view starts at its first cell, scrolled to the top
	}
	if fewer {
		tv.keepScrollInRows()
	}
	tv.table.Refresh()
	tv.resolvePending()
}

// keepScrollInRows brings the table's scroll offset back within its rows after the list got
// shorter, as a scroller does with its own: down to where the new last row ends at the bottom, to
// the top when every row fits. Fyne 2.8's table keeps its offset where the longer list left it and
// draws the rows from there, past the new last one: a namespace filter that narrowed a list
// scrolled down to two pods showed "2 of 200 items" and no row (the owner's finding of 2026-10-22,
// acme-d-pinniped). Its scroller checks its offset again only when it is resized, and then sets an
// offset that no longer fits to zero without telling the table, which keeps its own; the table's
// ScrollToOffset sets both.
func (tv *tableView) keepScrollInRows() {
	b := tv.table.body
	if b == nil || b.Content == nil || b.Size().Height <= 0 {
		return
	}
	last := max(b.Content.MinSize().Height-b.Size().Height, 0)
	tv.table.ScrollToOffset(fyne.NewPos(b.Offset.X, min(b.Offset.Y, last)))
	tv.table.offset = b.Offset // the scroller may not report a reset to zero
}

// refreshAges re-renders the visible cells so that age columns advance and absolute times follow
// the Preferences' time zone.
func (tv *tableView) refreshAges() {
	tv.alerts.refreshAges() // the alert board's since texts (alertboard.go)
	if tv.snap == nil {
		return
	}
	tv.now, tv.tf = tv.sh.now(), tv.sh.timeFormat()
	tv.table.Refresh()
}

// stateChanged follows namespace, filter, status filter and sort changes made anywhere (the user,
// an agent).
func (tv *tableView) stateChanged() {
	tv.alerts.request() // the board's search, namespaces or folding changed (while it shows)
	tv.syncSearch()
	tv.toggles.sync()
	tv.updateNamespaceButton()
	tv.syncNamespaceChecks()
	tv.states.update()
}

// selectionChanged re-renders the checked rows. It does not scroll: a checkbox, ⌘- and ⇧-click
// change the selection of rows the user sees, and the first selected row may be far away
// (drawerChanged scrolls to the drawer's row).
func (tv *tableView) selectionChanged(sel []model.ObjectRef) {
	tv.selected = slices.Clone(sel)
	tv.check.setSelection(sel)
	tv.marksChanged()
	tv.checksChanged()
}

// drawerChanged follows the drawer's object (UIState.Drawer): its row is highlighted, checked or
// not (A73: a plain click opens the drawer and keeps the checked rows checked), and it is the
// scroll target: an agent's ui.navigate opens an object that may be far down the table, or in a
// view whose snapshot has not arrived yet.
func (tv *tableView) drawerChanged(d *model.ObjectRef) {
	if d != nil {
		ref := *d
		tv.pending = &ref
	} else {
		tv.pending = nil
	}
	tv.marksChanged()
	tv.resolvePending()
}

// marksChanged re-renders the rows' highlight and checkboxes: the cells the table has made.
func (tv *tableView) marksChanged() {
	for _, c := range tv.cells {
		c.setMarks(tv.rowSelected(c.row), tv.rowChecked(c.row))
	}
}

// resolvePending scrolls to the pending object once the shown snapshot contains it.
func (tv *tableView) resolvePending() {
	if tv.pending == nil || tv.snap == nil || tv.snap.View != tv.pending.Kind {
		return
	}
	i := tv.snap.IndexOf(*tv.pending)
	if i < 0 {
		return
	}
	tv.pending = nil
	tv.scrollTo(i)
}

// scrollTo scrolls a row into view and refreshes the cells: ScrollTo alone moves the offset but
// leaves the old cells in place until the next refresh.
func (tv *tableView) scrollTo(row int) {
	tv.copyIcon.moving()
	tv.layoutTable()
	tv.table.ScrollTo(widget.TableCellID{Row: row, Col: 0})
	tv.table.Refresh()
}

// layoutTable lays the table out for its current row count. Fyne 2.8 lays out a table's scroller
// only when the table is resized, and the scroller clamps its offset against its content's laid-out
// size: right after the row count grew (a new view, a cleared filter), ScrollTo sets the table's
// offset to the row but the scroller clamps its own to the old, smaller content and the cells are
// drawn outside the viewport. Resizing by a point and back forces the layout.
func (tv *tableView) layoutTable() {
	sz := tv.table.Size()
	if sz.IsZero() {
		return
	}
	tv.table.Resize(sz.Add(fyne.NewSize(0, 1)))
	tv.table.Resize(sz)
}

// rowSelected reports whether a row is highlighted: checked, or the drawer's object. O(1).
func (tv *tableView) rowSelected(row int) bool {
	if tv.snap == nil || row < 0 || row >= len(tv.snap.Rows) {
		return false
	}
	if d := tv.sh.ui.Drawer; d != nil && keyOf(*d) == keyOf(tv.snap.Rows[row].Ref) {
		return true
	}
	return tv.rowChecked(row)
}

// rowChecked reports whether a row is in the selection: its checkbox is checked. O(1).
func (tv *tableView) rowChecked(row int) bool {
	if tv.snap == nil || row < 0 || row >= len(tv.snap.Rows) || len(tv.selected) == 0 {
		return false
	}
	return tv.check.has(tv.snap.Rows[row].Ref)
}

// length, createCell and updateCell are the table's data callbacks. The table has two columns
// more than the view: the row's checkbox first (checkColumn, tablecheck.go), the ⋮ that opens each
// row's actions menu last.
func (tv *tableView) length() (int, int) {
	if tv.snap == nil {
		return 0, len(tv.cols)
	}
	return len(tv.snap.Rows), len(tv.cols) + 2
}

// menuColumn is the table column of the ⋮.
func (tv *tableView) menuColumn() int { return firstDataColumn + len(tv.cols) }

func (tv *tableView) createCell() fyne.CanvasObject { return newTableCell(tv) }

// updateCell fills one visible cell. It is O(1): an index into the snapshot, an age formatted
// against the refresh's clock, and text and colour set on canvas objects the cell reuses.
func (tv *tableView) updateCell(id widget.TableCellID, o fyne.CanvasObject) {
	c := o.(*tableCell)
	if !c.tracked {
		// Cells are registered on their first update, not on creation: Table.Refresh creates a
		// template cell to measure each time, and those are never shown or updated.
		c.tracked = true
		tv.cells = append(tv.cells, c)
	}
	if tv.snap == nil || id.Row < 0 || id.Row >= len(tv.snap.Rows) || id.Col < 0 || id.Col > tv.menuColumn() {
		c.set(id.Row, id.Col, cellContent{}, rowMarks{}, false, false)
		tv.copyIcon.updated(c)
		return
	}
	var content cellContent
	switch id.Col {
	case tv.menuColumn():
		content.kind = cellMenu
	case checkColumn:
		content.kind = cellCheck
	default:
		row := &tv.snap.Rows[id.Row]
		ci := tv.cols[id.Col-firstDataColumn]
		col := &tv.snap.Columns[ci]
		var cell model.Cell
		if ci < len(row.Cells) {
			cell = row.Cells[ci]
		}
		status := cell.Status
		if status == model.StatusNone && col.ID == columnStatus {
			status = tv.snap.Rows[id.Row].Status
		}
		content = cellContent{text: tv.tf.CellText(*col, cell, tv.now), status: status, link: cell.Link, parts: cell.Parts, marks: cell.Marks, bar: cell.Bar, tip: cell.Tip}
		if content.link != nil && !tv.serves(content.link.Kind) {
			content.link = nil // nowhere to go: plain text
		}
		// A word is a pill, a sentence is not: an event's message in a status colour stays plain text.
		content.pill = status != model.StatusNone && col.Type == model.ColumnText && col.ID != columnMessage &&
			utf8.RuneCountInString(content.text) <= maxPillText
		if col.Type == model.ColumnWarning {
			content.kind = cellWarning
			if tv.statusLit(row) {
				content.kind, content.text = cellText, "" // the spine and the pill say it; the reason moves to the status word's tooltip
			}
		}
		if col.ID == columnStatus && content.tip == "" && tv.statusLit(row) {
			content.tip = tv.warningReason(row)
		}
		if col.Type == model.ColumnIcon {
			content.kind, content.text = cellImage, cell.Image
		}
		if col.Type == model.ColumnGrade {
			content.kind, content.grade = cellGrade, cell.Grade // the shield; the text is its tooltip (gradebadge.go)
		}
	}
	content.row = tv.snap.Rows[id.Row].Status
	hover := id.Row == tv.hoverRow
	marks := rowMarks{selected: tv.rowSelected(id.Row), checked: tv.rowChecked(id.Row)}
	if c.set(id.Row, id.Col, content, marks, hover, hover && id.Col == tv.hoverCol) && tv.sh.tip.owner == c {
		tv.sh.tip.hide() // the cell under the tooltip shows something else now
	}
	tv.copyIcon.updated(c)
	c.setFocused(tv.keyboardOn(id.Row, id.Col))
}

// statusLit reports whether the row shows a status pill and a spine, so that its warning icon would
// say the same a third time: the row is in an abnormal state and a status column is shown.
func (tv *tableView) statusLit(row *model.Row) bool {
	if !abnormalStatus(row.Status) {
		return false
	}
	for _, ci := range tv.cols {
		if tv.snap.Columns[ci].ID == columnStatus {
			return true
		}
	}
	return false
}

// warningReason is the text of the row's warning cell, the reason of the icon that a lit row does
// not draw (statusLit): it is the status word's tooltip then.
func (tv *tableView) warningReason(row *model.Row) string {
	for _, ci := range tv.cols {
		if tv.snap.Columns[ci].Type == model.ColumnWarning && ci < len(row.Cells) {
			return tv.tf.CellText(tv.snap.Columns[ci], row.Cells[ci], tv.now)
		}
	}
	return ""
}

func (tv *tableView) createHeader() fyne.CanvasObject {
	h := newHeaderCell(tv.headerTapped)
	h.onDrag, h.onDragEnd = tv.order.dragged, tv.order.ended // a column's header moves it (columnorder.go)
	return h
}

// updateHeader fills a column header and notices widths the user dragged: Fyne has no column
// resize callback, but it resizes the header before updating it.
func (tv *tableView) updateHeader(id widget.TableCellID, o fyne.CanvasObject) {
	h := o.(*headerCell)
	if !h.tracked { // registered on its first update, as the body cells: Table.Refresh makes templates it never shows
		h.tracked = true
		tv.headers = append(tv.headers, h)
	}
	if id.Row == -1 && tv.snap != nil && id.Col == checkColumn {
		tv.check.showHeader(h) // the header's checkbox: every row shown, or none
		return
	}
	vis, data := tv.dataColumn(id.Col)
	if id.Row != -1 || tv.snap == nil || !data {
		// The ⋮ column's header is the ⋮ that shows and hides columns.
		h.setMenu(id.Col, id.Row == -1 && tv.snap != nil && id.Col == tv.menuColumn())
		return
	}
	col := tv.snap.Columns[tv.cols[vis]]
	sort := 0
	if s := tv.snap.Sort; s.Column == col.ID {
		sort = 1
		if s.Desc {
			sort = -1
		}
	}
	title := col.Title
	if col.Type == model.ColumnWarning || col.Type == model.ColumnIcon || col.Type == model.ColumnGrade {
		title = "" // Lens's warning and icon columns, and the grades', have no title; Title names them for agents
	}
	h.set(id.Col, title, sort)
	h.setFixed(fixedWidth(col))
	if !tv.fitting && !fixedWidth(col) && id.Col < len(tv.setWidths) {
		if w := o.Size().Width; w > 0 && w != tv.setWidths[id.Col] {
			tv.setWidths[id.Col] = w
			tv.userWidths[tv.snap.View+"/"+col.ID] = w
		}
	}
}

// headerTapped sorts by a column; tapping the sorted column again reverses the order. The ⋮ at the
// end of the header row opens the column list.
func (tv *tableView) headerTapped(col int) {
	if tv.snap != nil && col == tv.menuColumn() {
		tv.openColumns()
		return
	}
	if tv.snap != nil && col == checkColumn {
		tv.toggleAll()
		return
	}
	vis, ok := tv.dataColumn(col)
	if tv.snap == nil || !ok || tv.snap.Columns[tv.cols[vis]].Type == model.ColumnIcon { // icons do not sort
		return
	}
	id := tv.snap.Columns[tv.cols[vis]].ID
	cur := tv.sh.ui.Sort
	tv.sh.dispatch("ui.sort", sortParams{Column: id, Desc: cur.Column == id && !cur.Desc}, nil)
}

// rowTapped opens the drawer on a row's object and keeps the checked rows checked (A73).
func (tv *tableView) rowTapped(id widget.TableCellID) {
	tv.table.UnselectAll() // the engine holds the selection; Fyne's own would block a second tap
	if tv.snap == nil || id.Row < 0 || id.Row >= len(tv.snap.Rows) {
		return
	}
	tv.setHoverCell(id.Row, id.Col)
	tv.sh.tip.hide()
	if id.Col == checkColumn { // Space or ↵ with the keyboard on a checkbox: ⇧ is the key's
		tv.checkTapped(id.Row, keyEventModifiers())
		return
	}
	ref := tv.snap.Rows[id.Row].Ref
	tv.check.anchor = &ref // ⇧-click selects from here (tablecheck.go)
	tv.sh.openDetails(ref)
}

// setHover moves the row highlight that follows the pointer and the keyboard.
func (tv *tableView) setHover(row int) { tv.setHoverCell(row, -1) }

// setHoverCell moves the row highlight and the hovered cell: a link underlines, and a cell that
// is cut off or a warning icon asks for its tooltip. It visits the cells the table has made (the
// visible ones and its pool), never the rows.
func (tv *tableView) setHoverCell(row, col int) {
	if row == tv.hoverRow && col == tv.hoverCol {
		return
	}
	oldRow := tv.hoverRow
	tv.hoverRow, tv.hoverCol = row, col
	for _, c := range tv.cells {
		if c.row == oldRow || c.row == row {
			c.setHover(c.row == row, c.row == row && c.col == col)
		}
	}
	tv.hoverCell = tv.cellUnderPointer(row, col)
	tv.copyIcon.follow(tv.hoverCell) // over the cell, which keeps its text (A69)
	tv.requestTip()
}

// cellUnderPointer returns the cell of a row and column that the pointer is on. The table's pool
// keeps cells with the ids they last showed, so the id alone may name a cell that is not on
// screen; the pointer decides. Only the cells with that id are measured.
func (tv *tableView) cellUnderPointer(row, col int) *tableCell {
	if !tv.pointerIn || row < 0 {
		return nil
	}
	drv := fyne.CurrentApp().Driver()
	for _, c := range tv.cells {
		if c.row != row || c.col != col || !c.Visible() {
			continue
		}
		p, s := drv.AbsolutePositionForObject(c), c.Size()
		if tv.pointer.X >= p.X && tv.pointer.X < p.X+s.Width && tv.pointer.Y >= p.Y && tv.pointer.Y < p.Y+s.Height {
			return c
		}
	}
	return nil
}

// requestTip asks for the tooltip of the cell under the pointer, or hides the tooltip.
func (tv *tableView) requestTip() {
	if tv.hoverCell == nil && tv.headerTipShown() {
		return // a column header's tooltip (tableheadertip.go)
	}
	if tv.hoverCell == nil || !tv.pointerIn {
		tv.sh.tip.hide()
		return
	}
	text := tv.hoverCell.tipText()
	if text == "" {
		tv.sh.tip.hide() // the pointer left the cell with the tooltip for one without
		return
	}
	tv.sh.tip.hover(tv.hoverCell, text, tv.pointer)
}

// cellTapped handles a click on a body cell: the ⋮ opens the row's actions menu below it, a link
// is followed (and the row is not selected), any other click selects the row through the table.
// mods are the click's own modifier keys (tableCell.MouseDown).
func (tv *tableView) cellTapped(c *tableCell, e *fyne.PointEvent, mods fyne.KeyModifier) {
	tv.sh.tip.hide()
	if tv.snap == nil || c.row < 0 || c.row >= len(tv.snap.Rows) {
		return
	}
	drv := fyne.CurrentApp().Driver()
	switch {
	case c.content.kind == cellMenu:
		tv.openRowMenu(c.row, drv.AbsolutePositionForObject(c).AddXY(0, c.Size().Height))
	case c.content.kind == cellCheck:
		tv.checkTapped(c.row, mods)
	case c.onLink(e.Position) && c.content.link.Same(tv.snap.Rows[c.row].Ref):
		// A link to the row's own object (an issue's name on the cluster Overview) opens its
		// details over the page, as a click on the row does.
		tv.sh.openDetails(tv.snap.Rows[c.row].Ref)
	case c.onLink(e.Position):
		vis, ok := tv.dataColumn(c.col)
		nsColumn := ok && tv.snap.Columns[tv.cols[vis]].ID == "namespace"
		tv.sh.followLink(*c.content.link, tv.snap.Rows[c.row].Ref.Context, nsColumn)
	case tv.modifiedClick(c.row, mods): // ⌘-click toggles the row, ⇧-click selects a range (tablecheck.go)
	default:
		if cnv := drv.CanvasForObject(tv.table); cnv != nil {
			cnv.Focus(tv.table)
		}
		tv.table.Select(widget.TableCellID{Row: c.row, Col: c.col})
	}
}

// columnWidth is a visible column's width: what the user dragged, else the profile's hint, else
// the default for the column id or type.
func (tv *tableView) columnWidth(i int) float32 {
	col := tv.snap.Columns[tv.cols[i]]
	if w, ok := tv.userWidths[tv.snap.View+"/"+col.ID]; ok && !fixedWidth(col) {
		return w
	}
	if col.Width > 0 {
		return col.Width
	}
	if w, ok := defaultColumnWidths[col.ID]; ok {
		return w
	}
	if w, ok := defaultWidthByType[col.Type]; ok {
		return w
	}
	return defaultWidthByType[model.ColumnText]
}

// fixedWidth reports whether a column keeps its width: Lens's warning column, an icon without a
// title, has no divider in the header and is neither dragged wider nor fitted to its content (the
// owner, 2026-10-21, after the dividers came back: "kann meiner Meinung nach eine feste Breite
// haben"); the image grades' shields too (the owner, 2026-10-22: "mit fester Breite hinter
// Containers", to save room as the warning column does).
func fixedWidth(c model.Column) bool {
	return c.Type == model.ColumnWarning || c.Type == model.ColumnGrade
}

// fixedAt reports whether the visible column i keeps its width (fixedWidth).
func (tv *tableView) fixedAt(i int) bool {
	return tv.snap != nil && i >= 0 && i < len(tv.cols) && fixedWidth(tv.snap.Columns[tv.cols[i]])
}

// flexColumn is the visible column that takes the free width: the name, as in Lens; for views
// without a visible name (Events hides it) the message; else the first column.
func (tv *tableView) flexColumn() int {
	for _, id := range []string{"name", columnMessage} {
		for i, ci := range tv.cols {
			if tv.snap.Columns[ci].ID == id {
				return i
			}
		}
	}
	return 0
}

// minFlexWidth is how narrow the flex column gets before the table scrolls sideways: since the
// checkbox column (2026-10-13) a view with many columns, such as Pods, gives up width of its name
// rather than push the ⋮ out of a window of 1440 points (Cogpit's, inferred).
const minFlexWidth = 140

// fitColumns gives every column its width and stretches the flex column to fill the frame, as
// Lens does, unless the user sized it; when the columns are wider than the frame, the flex column
// gives up width down to minFlexWidth first.
func (tv *tableView) fitColumns(width float32) {
	if tv.snap == nil || len(tv.cols) == 0 || width <= 0 {
		return
	}
	if width == tv.fitWidth && len(tv.setWidths) == len(tv.cols)+2 {
		return
	}
	tv.fitWidth = width
	tv.copyIcon.moving() // the cells move under the pointer
	th := tv.table.Theme()
	gap := th.Size(theme.SizeNamePadding)
	flex := tv.flexColumn()
	// One width per table column: the checkbox, the view's columns, the ⋮.
	widths := make([]float32, len(tv.cols)+2)
	widths[checkColumn] = th.Size(sizeMenuColumn)
	widths[tv.menuColumn()] = th.Size(sizeMenuColumn)
	others := widths[checkColumn] + gap + widths[tv.menuColumn()] + gap
	for i := range tv.cols {
		widths[firstDataColumn+i] = tv.columnWidth(i)
		if i != flex {
			others += widths[firstDataColumn+i] + gap
		}
	}
	col := tv.snap.Columns[tv.cols[flex]]
	if _, user := tv.userWidths[tv.snap.View+"/"+col.ID]; !user {
		fill := width - others - th.Size(theme.SizeNameScrollBar)
		switch w := &widths[firstDataColumn+flex]; {
		case fill > *w:
			*w = fill
		case fill < *w:
			*w = max(fill, min(*w, minFlexWidth))
		}
	}
	if len(tv.setWidths) != len(widths) {
		tv.setWidths = make([]float32, len(widths))
	}
	// Each SetColumnWidth refreshes the table, and the header updates in between see columns
	// that are not set yet; fitting tells updateHeader that none of this is the user dragging.
	tv.fitting = true
	defer func() { tv.fitting = false }()
	for i, w := range widths {
		if tv.setWidths[i] != w {
			tv.setWidths[i] = w
			tv.table.SetColumnWidth(i, w)
		}
	}
}

// stretchLayout lays the table out at full size and refits the columns when its width changes.
type stretchLayout struct{ tv *tableView }

func (l *stretchLayout) MinSize(objs []fyne.CanvasObject) fyne.Size { return objs[0].MinSize() }

func (l *stretchLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	objs[0].Move(fyne.NewPos(0, 0))
	objs[0].Resize(size)
	l.tv.fitColumns(size.Width)
}

// searchChanged debounces typing: the filter goes to the engine once the user pauses.
func (tv *tableView) searchChanged(text string) {
	tv.toggles.sync() // the field's text may make the pattern valid or not
	if tv.syncing2 {
		return
	}
	if tv.searchTimer != nil && tv.searchTimer.Stop() {
		tv.filterBusy--
	}
	tv.filterBusy++
	tv.searchTimer = time.AfterFunc(searchDebounce, func() {
		tv.sh.dispatch("ui.setFilter", filterParams{Filter: text}, func(_ any, err error) {
			tv.filterBusy--
			tv.sh.showError(err)
			tv.syncSearch()
		})
	})
}

// dropSearch drops a search typed but not sent yet and shows the engine's: the active tab or the
// view changed, and ui.setFilter, which applies to the active tab, would put it into another one.
func (tv *tableView) dropSearch() {
	if tv.searchTimer != nil && tv.searchTimer.Stop() {
		tv.filterBusy--
		tv.syncSearch()
	}
}

// syncSearch shows the engine's filter in the search entry, unless the user is still typing.
func (tv *tableView) syncSearch() {
	if tv.filterBusy > 0 || tv.search.Text == tv.sh.ui.Filter {
		return
	}
	tv.syncing2 = true
	tv.search.SetText(tv.sh.ui.Filter)
	tv.syncing2 = false
}

// updateNamespaceButton labels the namespace filter: "All namespaces", one name, or a count; on
// the Definitions view the same button is the API group filter ("All groups").
func (tv *tableView) updateNamespaceButton() {
	all, _, sel := tv.filterChoices()
	text := all
	switch {
	case len(sel) == 1:
		text = sel[0]
	case len(sel) > 1:
		noun := "namespaces"
		if tv.groupMode() {
			noun = "groups"
		}
		text = formatCount(int64(len(sel))) + " " + noun
	}
	if tv.nsButton.Text != text {
		tv.nsButton.SetText(text)
	}
	// Red while a namespace it names is deleted or terminating (GW9, nsgone.go).
	if alarm := !tv.groupMode() && tv.sh.filterNamesGone(); alarm != tv.nsTintTheme.on {
		tv.nsTintTheme.on = alarm
		tv.nsTint.Refresh()
	}
	// Lens shows no namespace filter on a cluster-scoped view (captures of Nodes, Ingress Classes,
	// Cluster Roles and Cluster Role Bindings, 2026-10-11), and a group filter on Definitions.
	if tv.groupMode() || tv.viewNamespaced() {
		tv.nsBox.Show()
	} else {
		tv.nsBox.Hide()
	}
}

// groupMode reports whether the filter button is the Definitions view's API group filter.
func (tv *tableView) groupMode() bool { return engine.FiltersByGroup(tv.sh.ui.View) }

// filterChoices returns what the filter button offers: the "all" entry's label, the names to
// choose from, and the engine's selection. Namespaces come from the cluster; API groups from its
// custom resource kinds (kinds.list), plus any group the selection holds.
func (tv *tableView) filterChoices() (all string, names, selected []string) {
	if !tv.groupMode() {
		return "All namespaces", tv.sh.namespaceChoices(tv.snap), tv.sh.ui.Namespaces // the accessible ones while Namespaces cannot be listed
	}
	set := map[string]bool{}
	for _, k := range tv.sh.kinds {
		if isCustomKind(k.Kind) {
			set[apiGroupOf(k)] = true
		}
	}
	for _, g := range tv.sh.ui.Groups {
		set[g] = true
	}
	for g := range set {
		names = append(names, g)
	}
	slices.Sort(names)
	return "All groups", names, tv.sh.ui.Groups
}

// viewNamespaced reports whether the open view's kind is namespaced: from its snapshot, before
// the first one from kinds.list, and true when neither knows (the filter then stays where it is
// until the view's snapshot says otherwise).
func (tv *tableView) viewNamespaced() bool {
	if tv.snap != nil && tv.snap.View == tv.sh.ui.View {
		return tv.snap.Namespaced
	}
	for _, k := range tv.sh.kinds {
		if k.Kind == tv.sh.ui.View {
			return k.Namespaced
		}
	}
	return true
}

// resourceTable is widget.Table with the hooks Fyne does not offer as callbacks: the pointer's
// position (for tooltips), the pointer leaving the table (clears the row highlight and the
// tooltip), scrolling (closes the tooltip), and Escape, which closes the drawer while the table
// has the focus.
type resourceTable struct {
	widget.Table
	tv     *tableView
	keying bool              // set while a key moves Fyne's highlight (tablekeys.go)
	shift  bool              // Shift is held, for ⇧Tab
	offset fyne.Position     // the scroll offset, as the scroller last reported it
	body   *container.Scroll // the rows' scroller below the header, for a click below the rows (tableclick.go)
	hover  fyne.Position     // the pointer in the table, for the cursor over a divider
	// sepClick is the last click on a header separator, for its double click (tableheader.go).
	sepClick click

	dragging, dragFixed bool // a drag is under way; it began on the divider of a column that keeps its width
}

func newResourceTable(tv *tableView) *resourceTable {
	t := &resourceTable{tv: tv}
	t.Length = tv.length
	t.CreateCell = tv.createCell
	t.UpdateCell = tv.updateCell
	t.ShowHeaderRow = true
	t.CreateHeader = tv.createHeader
	t.UpdateHeader = tv.updateHeader
	// Fyne's dividers draw a grid and its selection marks one cell; Cogpit's cells draw row
	// lines and a full-row highlight themselves. HideSeparators hides both of Fyne's.
	t.HideSeparators = true
	t.OnSelected = tv.rowTapped
	t.OnHighlighted = tv.highlighted
	t.ExtendBaseWidget(t)
	return t
}

// CreateRenderer adds a scroll hook to the table's: a scroll moves the cells under the pointer,
// so the tooltip closes; the offset is kept for finding the column separator under a double click.
func (t *resourceTable) CreateRenderer() fyne.WidgetRenderer {
	r := t.Table.CreateRenderer()
	for _, o := range r.Objects() {
		if s, ok := o.(*container.Scroll); ok && s.OnScrolled != nil {
			t.body = s
			inner := s.OnScrolled
			s.OnScrolled = func(p fyne.Position) {
				inner(p)
				t.offset = p
				t.tv.sh.tip.hide()
				t.tv.copyIcon.moving()
			}
		}
	}
	return r
}

// MouseIn records the pointer for tooltips.
func (t *resourceTable) MouseIn(ev *desktop.MouseEvent) {
	t.hover = ev.Position
	t.tv.pointer, t.tv.pointerIn = ev.AbsolutePosition, true
	t.Table.MouseIn(ev)
}

// Cursor implements desktop.Cursorable: Fyne's resize arrows over a header divider, but none over
// the edge of a column that keeps its width (fixedWidth).
func (t *resourceTable) Cursor() desktop.Cursor {
	c := t.Table.Cursor()
	if c == desktop.HResizeCursor && t.tv.fixedAt(t.tv.separatorAt(t.hover)) {
		return desktop.DefaultCursor
	}
	return c
}

// Dragged implements fyne.Draggable: Fyne's table drags a column's divider, except a column's that
// keeps its width (fixedWidth); where the drag began decides it.
func (t *resourceTable) Dragged(e *fyne.DragEvent) {
	if !t.dragging {
		t.dragging = true
		t.dragFixed = t.tv.fixedAt(t.tv.separatorAt(e.Position.Subtract(e.Dragged)))
	}
	if !t.dragFixed {
		t.Table.Dragged(e)
	}
}

// DragEnd implements fyne.Draggable.
func (t *resourceTable) DragEnd() {
	t.dragging, t.dragFixed = false, false
	t.Table.DragEnd()
}

// MouseMoved records the pointer; the table's hover callback then moves the highlight.
func (t *resourceTable) MouseMoved(ev *desktop.MouseEvent) {
	t.hover = ev.Position
	t.tv.pointer, t.tv.pointerIn = ev.AbsolutePosition, true
	t.Table.MouseMoved(ev)
	t.tv.requestTip()
	t.tv.copyIcon.moved()
}

// MouseOut clears the row highlight and the tooltip when the pointer leaves the table.
func (t *resourceTable) MouseOut() {
	t.Table.MouseOut()
	t.tv.pointerIn = false
	t.tv.setHover(-1)
	t.tv.sh.tip.hide()
}

// cellKind says what a body cell draws.
type cellKind uint8

const (
	cellText    cellKind = iota // text, possibly a link or in coloured parts
	cellWarning                 // Lens's warning icon; the text is the icon's tooltip
	cellMenu                    // the ⋮ that opens the row's actions menu
	cellCheck                   // the row's checkbox (tablecheck.go)
	cellImage                   // an image from a URL, which text holds (a chart's icon, helmcharticons.go)
	cellGrade                   // an image grade's shield; the text and the tip are its tooltip (gradebadge.go)
)

// cellContent is what one body cell shows. Its fields are values or pointers into the immutable
// snapshot, so building and comparing one allocates nothing.
type cellContent struct {
	text   string
	status model.Status
	link   *model.ObjectRef
	parts  *[]model.CellPart
	marks  *[]model.CellMark // squares in place of the text (a pod's containers)
	bar    *model.CellBar    // a usage bar beside the text (a node's CPU, Memory, Disk; tablebar.go)
	tip    string            // the cell's own tooltip (model.Cell.Tip: a kubelet's support, D-91)
	grade  model.ImageGrade  // a grade cell's shield
	kind   cellKind
	row    model.Status // the row's status: its spine and tint (list_rows.go)
	pill   bool         // a status cell: a healthy word with a dot, an abnormal one in a pill
}

// tableCell is one body cell: text in the cell's status colour (a link in the link colour, a cell
// in coloured parts run by run), or an icon (the warning triangle, the ⋮), and a background and
// bottom line that reach into the table's cell gaps so that a row reads as one band.
//
// The cell handles its own taps, so that a click on a link follows it instead of selecting the
// row; any other click selects the row through the table, as before. A right-click opens the
// row's actions menu.
type tableCell struct {
	widget.BaseWidget
	tv    *tableView
	bg    *canvas.Rectangle
	text  *canvas.Text        // the text, or the first part
	more  []*canvas.Text      // the further parts of a cell in coloured parts; grown on demand
	runX  []float32           // each part's x offset, set by fit
	marks []*canvas.Rectangle // the squares of a cell in marks; grown on demand
	shown int                 // the squares that fit, set by fit
	bar   *cellBar            // a usage bar's track and fill; made when the cell first shows one (tablebar.go)
	icon  *canvas.Image       // made when the cell first shows an icon
	badge *gradeBadge         // an image grade's shield; made when the cell first shows one
	line  *canvas.Rectangle
	// The Dark Cockpit's row marks (list_rows.go), each made the first time a cell needs it.
	spine  *canvas.Rectangle // an abnormal row's status spine, in the first cell
	selBar *canvas.Rectangle // the selected row's glow blue bar, in the first cell
	pill   *canvas.Rectangle // a status cell's pill, filled for an abnormal state
	dot    *canvas.Circle    // a status cell's dot
	under  *canvas.Rectangle // a hovered link's underline; made when the cell first shows a link
	ring   *canvas.Rectangle // the keyboard's focus ring; made when the keyboard first reaches the cell
	objs   int               // bumped when an object is added, so the renderer lists it

	content   cellContent
	full      string // the cell text; text.Text may be a shortened form of it
	fitWidth  float32
	truncated bool // the shown text is shorter than full: the tooltip shows full
	iconRes   fyne.Resource
	selected  bool // the row is highlighted: checked, or the drawer's object
	checked   bool // the row is in the selection: its checkbox is checked
	hover     bool // the pointer or the keyboard is on the row
	linkHover bool // the pointer is on this cell, which is a link
	focused   bool // the keyboard is on this cell (tablekeys.go)
	tracked   bool // registered in tableView.cells
	row, col  int
	// pressMods are the modifier keys of the primary button's press on this cell, from its own
	// mouse event (MouseDown), until the click's Tapped takes them.
	pressMods fyne.KeyModifier
}

var (
	_ fyne.Tappable          = (*tableCell)(nil)
	_ fyne.SecondaryTappable = (*tableCell)(nil)
	_ desktop.Mouseable      = (*tableCell)(nil)
	_ desktop.Cursorable     = (*tableCell)(nil)
)

func newTableCell(tv *tableView) *tableCell {
	c := &tableCell{
		tv:   tv,
		bg:   canvas.NewRectangle(color.Transparent),
		text: canvas.NewText("", color.Transparent),
		line: canvas.NewRectangle(color.Transparent),
		row:  -1, col: -1,
	}
	c.ExtendBaseWidget(c)
	return c
}

// rowMarks are how a row is marked: highlighted (selected: checked, or the drawer's object) and
// checked (in the selection). Since A73 they differ: a plain click opens the drawer on a row and
// keeps the checked rows, so the drawer's row is highlighted without being checked.
type rowMarks struct{ selected, checked bool }

// set gives the cell new content; unchanged content costs a few comparisons. It reports whether
// the content changed.
//
// A cell whose content changed is laid out again at its size: Fyne's table gives a cell it keeps
// on screen (a refresh after a filter, a watch event that moves the rows) new content without a
// layout when the cell's size is unchanged, and the GL driver lays a cell out only when one of its
// objects' minimum size changed. What Layout alone shows, hides and places (a pod's squares, a
// usage bar, the count after the squares) would otherwise stay as the cell's last content left it:
// a colleague of the owner saw a pod with three squares of which the third was the pod's before
// (2026-10-19). Laying out here keeps one rule for every object a cell draws, a new kind of mark
// included (architecture.md §4.4, the lead's choice of 2026-10-19).
func (c *tableCell) set(row, col int, content cellContent, marks rowMarks, hover, linkHover bool) bool {
	c.row, c.col = row, col
	changed := false
	if content != c.content {
		c.content, c.full, c.fitWidth = content, content.text, -1
		changed = true
	}
	linkHover = linkHover && content.link != nil
	if changed || marks.selected != c.selected || marks.checked != c.checked || hover != c.hover || linkHover != c.linkHover {
		c.selected, c.checked, c.hover, c.linkHover = marks.selected, marks.checked, hover, linkHover
		c.apply()
	}
	if changed {
		c.layout(c.Size())
	}
	return changed
}

// setMarks highlights the cell's row (selected) and checks its checkbox (checked).
func (c *tableCell) setMarks(selected, checked bool) {
	if selected != c.selected || checked != c.checked {
		c.selected, c.checked = selected, checked
		c.apply()
	}
}

// setFocused rings the cell while the keyboard is on it.
func (c *tableCell) setFocused(on bool) {
	if on == c.focused {
		return
	}
	c.focused = on
	if c.ring == nil {
		c.ring = canvas.NewRectangle(color.Transparent)
		c.objs++
	}
	c.Refresh() // applies the ring's colour and lays it out; two cells per key press
}

// setHover marks the row as hovered and, for a link, the cell itself.
func (c *tableCell) setHover(row, cell bool) {
	cell = cell && c.content.link != nil
	if row != c.hover || cell != c.linkHover {
		c.hover, c.linkHover = row, cell
		c.apply()
	}
}

// MouseDown implements desktop.Mouseable: it keeps the modifier keys of the primary button's
// press for the click it begins. Fyne's GLFW driver sends the press to the object the click's Tapped
// goes to, with the OS's modifier flags of that very event (on macOS NSEvent's modifierFlags), so
// ⌘ and ⇧ are what the user held while clicking, not what the driver last saw in a key event.
func (c *tableCell) MouseDown(ev *desktop.MouseEvent) {
	if ev.Button == desktop.MouseButtonPrimary {
		c.pressMods = clickModifiers(ev.Modifier)
	}
}

// MouseUp implements desktop.Mouseable; the click is Tapped's.
func (c *tableCell) MouseUp(*desktop.MouseEvent) {}

// Tapped implements fyne.Tappable: follow a link, open the menu of the ⋮, else select the row. It
// takes the press's modifier keys, so a tap without a press (Fyne's test driver) is a plain click.
func (c *tableCell) Tapped(e *fyne.PointEvent) {
	mods := c.pressMods
	c.pressMods = 0
	c.tv.cellTapped(c, e, mods)
}

// TappedSecondary implements fyne.SecondaryTappable: a right-click opens the row's actions menu
// (Lens since 2025.10.230725).
func (c *tableCell) TappedSecondary(e *fyne.PointEvent) { c.tv.openRowMenu(c.row, e.AbsolutePosition) }

// Cursor implements desktop.Cursorable: a hand over a link the pointer is on and over the ⋮.
func (c *tableCell) Cursor() desktop.Cursor {
	if c.linkHover || c.content.kind == cellMenu || c.content.kind == cellCheck {
		return desktop.PointerCursor
	}
	return desktop.DefaultCursor
}

// onLink reports whether a point in the cell lies on its link text.
func (c *tableCell) onLink(p fyne.Position) bool {
	if c.content.link == nil || c.text.Text == "" {
		return false
	}
	x := c.text.Position().X
	return p.X >= x && p.X <= x+c.text.Size().Width
}

// tipText is the cell's tooltip: a warning's reason, the cell's own (model.Cell.Tip), or the full
// text of a cut-off cell.
func (c *tableCell) tipText() string {
	switch {
	case c.content.kind == cellWarning:
		return c.full
	case c.content.kind == cellGrade && c.content.tip != "":
		return c.full + "\n" + c.content.tip // "D · 2 Critical", then the counts behind it
	case c.content.kind == cellGrade:
		return c.full
	case c.content.kind == cellText && c.content.marks != nil:
		lines := make([]string, 0, len(*c.content.marks))
		for _, m := range *c.content.marks {
			lines = append(lines, m.Text)
		}
		return strings.Join(lines, "\n") // a square stands for a container: say which, and how it is
	case c.content.kind == cellText && c.content.bar != nil && c.content.bar.Detail != "":
		return c.content.bar.Detail + " (" + c.full + ")" // the amounts behind the bar
	case c.content.kind == cellText && c.content.tip != "" && c.truncated:
		return c.full + "\n" + c.content.tip // the text cut off, then the cell's own tooltip
	case c.content.kind == cellText && c.content.tip != "":
		return c.content.tip
	case c.content.kind == cellText && c.truncated:
		return c.full
	}
	return ""
}

// apply recolours and refits the cell's canvas objects.
func (c *tableCell) apply() {
	th := c.Theme()
	c.bg.FillColor = c.rowPlate(th)
	size := th.Size(theme.SizeNameText)
	fg := statusColorName(c.content.status)
	if c.pillShows() && !abnormalStatus(c.content.status) {
		fg = theme.ColorNameForeground // a healthy word is quiet; its dot carries the colour
	}
	if c.content.link != nil {
		fg = colorLink
	}
	if c.selected && (fg == colorLink || fg == theme.ColorNameForeground) {
		fg = colorRowSelectedText // the plain and link colours are too faint on the selection
	}
	c.text.Color = themeColor(th, fg)
	if c.pillShows() && abnormalStatus(c.content.status) && !c.selected {
		c.text.Color = pillInk(th, themeColor(th, fg), pillFill(th)) // 4.5:1 on its own tint (chrome_tones.go)
	}
	if c.content.link != nil && !c.selected && !c.linkHover {
		// A link is quiet until the pointer is on it: three blue columns would tint every healthy row
		// and compete with the selection (Dark Cockpit: blue is you, focus and selection).
		c.text.Color = withAlpha(themeColor(th, theme.ColorNameForeground), 0xeb)
	}
	c.text.TextSize = size
	if c.content.parts != nil && c.content.kind == cellText {
		parts := *c.content.parts
		for len(c.more) < len(parts)-1 {
			c.more = append(c.more, canvas.NewText("", color.Transparent))
		}
		if len(parts) > 0 {
			c.text.Color = themeColor(th, statusColorName(parts[0].Status))
		}
		for i, t := range c.more {
			t.TextSize = size
			if i+1 < len(parts) {
				t.Color = themeColor(th, statusColorName(parts[i+1].Status))
			}
		}
	}
	if c.content.marks != nil && c.content.kind == cellText {
		marks := *c.content.marks
		for len(c.marks) < len(marks) {
			c.marks = append(c.marks, canvas.NewRectangle(color.Transparent))
			c.objs++
		}
		for i, r := range c.marks {
			if i < len(marks) {
				mc := markColor(th, marks[i].Status)
				paintMark(r, mc, marks[i].Hollow || marks[i].Status == model.StatusStopped)
			}
		}
	}
	c.applyBar(th)
	c.applyRowMarks(th)
	if c.under == nil && c.content.link != nil {
		c.under = canvas.NewRectangle(color.Transparent)
		c.objs++
	}
	if c.under != nil {
		c.under.FillColor = themeColor(th, colorLink)
		if c.linkHover {
			c.under.Show()
		} else {
			c.under.Hide()
		}
		c.under.Refresh()
	}
	c.line.FillColor = themeColor(th, theme.ColorNameSeparator)
	if c.ring != nil {
		c.ring.FillColor = color.Transparent
		c.ring.StrokeColor = themeColor(th, colorAccent)
		c.ring.StrokeWidth = th.Size(theme.SizeNameInputBorder)
		c.ring.CornerRadius = th.Size(theme.SizeNameSelectionRadius)
		if c.focused {
			c.ring.Show()
		} else {
			c.ring.Hide()
		}
		c.ring.Refresh()
	}
	c.setIcon(c.tv.cellIcon(c.content, c.checked))
	c.setGrade()
	c.fit(th)
	c.colourMore(th)
	c.bg.Refresh()
	c.text.Refresh()
	for _, t := range c.more {
		t.Refresh()
	}
	for _, r := range c.marks {
		r.Refresh()
	}
	c.refreshBar()
}

// setGrade shows a grade cell's shield, or hides the shield of a cell that shows something else.
func (c *tableCell) setGrade() {
	if c.content.kind != cellGrade || c.content.grade == model.GradeNone {
		if c.badge != nil && c.badge.Visible() {
			c.badge.Hide()
		}
		return
	}
	if c.badge == nil {
		c.badge = newGradeBadge(gradeTableSide, c.content.grade)
		c.objs++
	}
	c.badge.setGrade(c.content.grade)
	if !c.badge.Visible() {
		c.badge.Show()
	}
}

// gradeTableSide is a grade's shield in a list's cell, in points (the owner's pick in the lab: 18).
const gradeTableSide = 18

// setIcon shows res, or hides the icon for nil. The image is refreshed (its SVG rasterised) only
// when the resource changes; the resources are shared per icon and colour (tableView.cellIcon).
func (c *tableCell) setIcon(res fyne.Resource) {
	if res == nil {
		if c.icon != nil && c.icon.Visible() {
			c.icon.Hide()
		}
		return
	}
	if c.icon == nil {
		c.icon = canvas.NewImageFromResource(nil)
		c.icon.FillMode = canvas.ImageFillContain
		c.objs++
	}
	if res != c.iconRes {
		c.iconRes = res
		c.icon.Resource = res
		c.icon.Refresh()
	}
	if !c.icon.Visible() {
		c.icon.Show()
	}
}

// fit shows the text, shortened with an ellipsis when it is wider than the cell; a cell in parts
// shows its runs side by side and shortens the run that reaches the edge. Measuring is cached by
// Fyne per string; the shortening runs only when the text or the width changed. It reports
// whether the shown text changed.
func (c *tableCell) fit(th fyne.Theme) bool {
	avail := c.Size().Width - 2*th.Size(sizeCellPadding)
	if avail == c.fitWidth {
		return false
	}
	c.fitWidth = avail
	avail = c.fitBar(th, avail) // a usage bar takes the room right of its share (tablebar.go)
	avail -= c.pillRoom()       // a status cell's pill takes its padding and dot (list_rows.go)
	if c.content.kind != cellText {
		c.truncated, c.shown = false, 0 // a reused cell drops the squares it drew before
		changed := c.text.Text != ""
		c.text.Text = ""
		c.hideMore(0)
		return changed
	}
	if c.content.marks != nil {
		more := c.fitMarks(th, avail)
		c.hideMore(0)
		changed := c.text.Text != more
		c.text.Text = more
		return changed
	}
	c.shown = 0
	if c.content.parts == nil || len(*c.content.parts) == 0 {
		shown := ellipsize(c.full, avail, c.text.TextSize, c.text.TextStyle)
		c.truncated = shown != c.full
		c.hideMore(0)
		if shown == c.text.Text {
			return false
		}
		c.text.Text = shown
		return true
	}
	parts := *c.content.parts
	space := fyne.MeasureText(" ", c.text.TextSize, c.text.TextStyle).Width
	c.runX = c.runX[:0]
	x := float32(0)
	c.truncated = false
	for i, p := range parts {
		t := c.text
		if i > 0 {
			t = c.more[i-1]
		}
		c.runX = append(c.runX, x)
		w := fyne.MeasureText(p.Text, t.TextSize, t.TextStyle).Width
		switch {
		case avail-x <= 0:
			t.Text, c.truncated = "", true
		case x+w > avail:
			t.Text, c.truncated = ellipsize(p.Text, avail-x, t.TextSize, t.TextStyle), true
		default:
			t.Text = p.Text
		}
		x += w + space
	}
	c.hideMore(len(parts) - 1)
	return true
}

// fitMarks decides how many of the cell's squares show in avail and returns the text after them:
// "+N" for the N that do not fit, empty when all do (the owner's F24 of 2026-10-17: a pod with more
// containers than the column is wide must not look as if the few shown were all it has). The
// squares give way until the count fits beside them; apply colours the count by the hidden
// containers' gravest state (moreStatus), so that "+1" in the Running colour tells a running
// container behind finished init containers.
//
// At most maxCellMarks squares show however wide the column is: a pod debugged often collects
// finished ephemeral containers (they stay as long as the pod), which come last and are counted
// (D-102).
func (c *tableCell) fitMarks(th fyne.Theme, avail float32) string {
	size, gap := markSize(th)
	marks := *c.content.marks
	n := len(marks)
	c.shown = max(0, min(n, maxCellMarks, int((avail+gap)/(size+gap)))) // a cell not laid out yet has no room
	c.truncated = c.shown < n
	if !c.truncated {
		return ""
	}
	for c.shown > 0 && float32(c.shown)*(size+gap)+fyne.MeasureText("+"+strconv.Itoa(n-c.shown), c.text.TextSize, c.text.TextStyle).Width > avail {
		c.shown--
	}
	return "+" + strconv.Itoa(n-c.shown)
}

// colourMore colours the "+N" of a cell whose squares do not all fit (fitMarks) by the hidden
// containers' gravest state.
func (c *tableCell) colourMore(th fyne.Theme) {
	if c.content.marks != nil && c.content.kind == cellText && c.truncated {
		c.text.Color = themeColor(th, markColorName(moreStatus((*c.content.marks)[c.shown:])))
	}
}

// maxCellMarks is how many squares a cell draws at most; the rest are counted (fitMarks).
// markStroke is a hollow square's outline.
const (
	maxCellMarks = 12
	markStroke   = 1.25
)

// paintMark colours a square: filled in col, or for a hollow mark (an ephemeral container that has
// finished, D-102) an outline in col on nothing.
func paintMark(r *canvas.Rectangle, col color.Color, hollow bool) {
	r.CornerRadius = markRadius
	if hollow {
		r.FillColor, r.StrokeColor, r.StrokeWidth = color.Transparent, col, markStroke
		return
	}
	r.FillColor, r.StrokeColor, r.StrokeWidth = col, color.Transparent, 0
}

// moreStatus is the gravest state among hidden squares: an error, a warning, one waiting, one
// running, an ephemeral container, else stopped.
func moreStatus(hidden []model.CellMark) model.Status {
	rank := map[model.Status]int{model.StatusError: 6, model.StatusWarning: 5, model.StatusPending: 4, model.StatusOK: 3,
		model.StatusEphemeral: 2, model.StatusStopped: 1}
	best := model.StatusStopped
	for _, m := range hidden {
		if rank[m.Status] > rank[best] {
			best = m.Status
		}
	}
	return best
}

// hideMore empties the part texts from index n on.
func (c *tableCell) hideMore(n int) {
	for i := n; i < len(c.more); i++ {
		c.more[i].Text = ""
	}
}

// ellipsize returns s, or its longest prefix plus "…" that fits in width.
func ellipsize(s string, width, size float32, style fyne.TextStyle) string {
	if s == "" || width <= 0 || fyne.MeasureText(s, size, style).Width <= width {
		return s
	}
	runes := []rune(s)
	lo, hi := 0, len(runes)
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if fyne.MeasureText(string(runes[:mid])+"…", size, style).Width <= width {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return string(runes[:lo]) + "…"
}

// CreateRenderer implements fyne.Widget.
func (c *tableCell) CreateRenderer() fyne.WidgetRenderer {
	c.apply()
	return &tableCellRenderer{c: c}
}

type tableCellRenderer struct {
	c     *tableCell
	objs  []fyne.CanvasObject
	nmore int
	gen   int
}

func (r *tableCellRenderer) Layout(size fyne.Size) { r.c.layout(size) }

// layout places the cell's objects in a cell of size: the renderer's Layout, and set's for a cell
// that got new content at an unchanged size.
func (c *tableCell) layout(size fyne.Size) {
	th := c.Theme()
	gap := th.Size(theme.SizeNamePadding)
	c.bg.Move(fyne.NewPos(-gap/2, -gap/2))
	c.bg.Resize(fyne.NewSize(size.Width+gap, size.Height+gap))
	c.line.Move(fyne.NewPos(-gap/2, size.Height+gap/2-1))
	c.line.Resize(fyne.NewSize(size.Width+gap, 1))
	if c.ring != nil {
		c.ring.Move(fyne.NewPos(-gap/2, -gap/2))
		c.ring.Resize(fyne.NewSize(size.Width+gap, size.Height+gap))
	}
	if c.fit(th) {
		c.colourMore(th)
		c.text.Refresh() // a column resize shortened or lengthened the text
		for _, t := range c.more {
			t.Refresh()
		}
	}
	pad := th.Size(sizeCellPadding)
	ts := c.text.MinSize()
	y := (size.Height - ts.Height) / 2
	x := c.layoutBar(th, size, pad, ts.Width) + c.pillLead()
	c.layoutRowMarks(size, ts, pad)
	if c.content.marks != nil && c.content.kind == cellText {
		ms, mg := markSize(th)
		x = pad + float32(c.shown)*(ms+mg) // the count of the squares that do not fit, after the shown (fitMarks)
	}
	c.text.Move(fyne.NewPos(x, y))
	c.text.Resize(ts)
	for i, t := range c.more {
		if i+1 < len(c.runX) && t.Text != "" {
			t.Move(fyne.NewPos(pad+c.runX[i+1], y))
			t.Resize(t.MinSize())
		}
	}
	if c.under != nil {
		c.under.Move(fyne.NewPos(pad, y+ts.Height-2))
		c.under.Resize(fyne.NewSize(ts.Width, 1))
	}
	if c.icon != nil {
		is := th.Size(sizeCellIcon)
		if c.content.kind == cellImage {
			is = chartIconSide(size.Height) // a chart's icon, as large as the row allows (helmcharticons.go)
		}
		c.icon.Move(fyne.NewPos((size.Width-is)/2, (size.Height-is)/2))
		c.icon.Resize(fyne.NewSize(is, is))
	}
	if c.badge != nil {
		c.badge.Move(fyne.NewPos((size.Width-gradeTableSide)/2, (size.Height-gradeTableSide)/2))
		c.badge.Resize(fyne.NewSize(gradeTableSide, gradeTableSide))
	}
	ms, mg := markSize(th)
	for i, r := range c.marks {
		if i >= c.shown {
			r.Hide()
			continue
		}
		in := r.StrokeWidth / 2 // an outline (a hollow mark) is drawn centred on the edge: inset, it is as large as a filled one
		r.Move(fyne.NewPos(pad+float32(i)*(ms+mg)+in, (size.Height-ms)/2+in))
		r.Resize(fyne.NewSize(ms-2*in, ms-2*in))
		r.Show()
	}
}

// markColorName is a square's colour: its status colour, but a running container (StatusOK) in the
// greener colorContainerRunning (owner, 2026-10-12).
func markColorName(s model.Status) fyne.ThemeColorName {
	if s == model.StatusOK {
		return colorContainerRunning
	}
	return statusColorName(s)
}

// markSize is the side of a cell's square and the gap between two, from the text size: a square
// sits in a row like a small letter.
func markSize(th fyne.Theme) (side, gap float32) {
	side = float32(int(th.Size(theme.SizeNameText)*0.66 + 0.5))
	return side, markGapWidth
}

func (r *tableCellRenderer) MinSize() fyne.Size {
	th := r.c.Theme()
	h := fyne.MeasureText("Mg", th.Size(theme.SizeNameText), fyne.TextStyle{}).Height
	return fyne.NewSize(2*th.Size(sizeCellPadding)+h, h+2*th.Size(sizeRowPadding))
}

func (r *tableCellRenderer) Refresh() {
	r.c.fitWidth = -1
	r.c.apply()
	r.Layout(r.c.Size())
	r.c.line.Refresh()
}

// Objects returns the cell's canvas objects; the list is rebuilt only when the cell made a new
// one (a part, its first icon or underline).
func (r *tableCellRenderer) Objects() []fyne.CanvasObject {
	c := r.c
	if r.objs == nil || r.nmore != len(c.more) || r.gen != c.objs {
		r.nmore, r.gen = len(c.more), c.objs
		r.objs = append(r.objs[:0], c.bg, c.line)
		if c.spine != nil {
			r.objs = append(r.objs, c.spine)
		}
		if c.selBar != nil {
			r.objs = append(r.objs, c.selBar)
		}
		if c.pill != nil {
			r.objs = append(r.objs, c.pill, c.dot)
		}
		r.objs = append(r.objs, c.text)
		if c.under != nil {
			r.objs = append(r.objs, c.under)
		}
		if c.icon != nil {
			r.objs = append(r.objs, c.icon)
		}
		if c.badge != nil {
			r.objs = append(r.objs, c.badge)
		}
		for _, t := range c.more {
			r.objs = append(r.objs, t)
		}
		for _, m := range c.marks {
			r.objs = append(r.objs, m)
		}
		if c.bar != nil {
			r.objs = append(r.objs, c.bar.track, c.bar.fill)
		}
		if c.ring != nil {
			r.objs = append(r.objs, c.ring)
		}
	}
	return r.objs
}

func (r *tableCellRenderer) Destroy() {}

// cellIcon returns the icon a cell shows: the warning triangle in the cell's status colour, the ⋮
// in the muted colour, the row's checkbox (checked on a checked row), or nil. The themed resources
// are made once per icon and colour, so that updating cells allocates nothing and a cell's image
// is re-rasterised only when its icon changes.
func (tv *tableView) cellIcon(content cellContent, checked bool) fyne.Resource {
	var key cellIconKey
	switch content.kind {
	case cellCheck:
		key = checkIconKey(checked)
	case cellWarning:
		if content.status == model.StatusNone {
			return nil
		}
		key = cellIconKey{theme.IconNameWarning, statusColorName(content.status)}
	case cellMenu:
		key = cellIconKey{theme.IconNameMoreVertical, colorMuted}
	case cellImage:
		return tv.sh.chartIconResource(content.text)
	default:
		return nil
	}
	if res, ok := tv.icons[key]; ok {
		return res
	}
	res := themedIcon(theme.Icon(key.icon), key.color)
	tv.icons[key] = res
	return res
}

// cellIconKey names a table icon in a colour.
type cellIconKey struct {
	icon  fyne.ThemeIconName
	color fyne.ThemeColorName
}

// headerCell is a column header: the title, an arrow on the sorted column, and a short divider at
// its right edge that marks where the column can be dragged wider. Tapping it sorts. The header of
// the ⋮ column shows a ⋮ instead, which opens the list of columns to show and hide.
type headerCell struct {
	widget.BaseWidget
	onTap     func(col int)
	onDrag    func(col int, ev *fyne.DragEvent) // dragging the header moves its column (columnorder.go)
	onDragEnd func(col int)
	col       int
	tracked   bool // registered in tableView.headers (tableheadertip.go)
	title     string
	sort      int  // 1 ascending, -1 descending, 0 not sorted
	menu      bool // the ⋮ at the end of the header row
	box       bool // the checkbox at the start of the header row (tablecheck.go)
	boxed     int  // the checkbox's state: checkNone, checkSome or checkAll
	fixed     bool // the column keeps its width (fixedWidth): no divider

	text  *canvas.Text
	arrow *iconImage
	sep   *canvas.Rectangle
	line  *canvas.Rectangle
}

func newHeaderCell(onTap func(int)) *headerCell {
	h := &headerCell{
		onTap: onTap, col: -1,
		text:  canvas.NewText("", color.Transparent),
		arrow: newIconImage(),
		sep:   canvas.NewRectangle(color.Transparent),
		line:  canvas.NewRectangle(color.Transparent),
	}
	h.ExtendBaseWidget(h)
	return h
}

func (h *headerCell) set(col int, title string, sort int) {
	h.col = col
	if title == h.title && sort == h.sort && !h.menu && !h.box {
		return
	}
	h.title, h.sort, h.menu, h.box = title, sort, false, false
	h.Refresh()
}

// setFixed marks the header of a column that keeps its width: it draws no divider.
func (h *headerCell) setFixed(on bool) {
	if on != h.fixed {
		h.fixed = on
		h.Refresh()
	}
}

// setMenu makes the cell the ⋮ of the header row (on), or an empty header.
func (h *headerCell) setMenu(col int, on bool) {
	h.col = col
	if on == h.menu && h.title == "" && h.sort == 0 && !h.box {
		return
	}
	h.title, h.sort, h.menu, h.box = "", 0, on, false
	h.Refresh()
}

// Cursor implements desktop.Cursorable: a hand over the ⋮ and the checkbox.
func (h *headerCell) Cursor() desktop.Cursor {
	if h.menu || h.box {
		return desktop.PointerCursor
	}
	return desktop.DefaultCursor
}

// Tapped implements fyne.Tappable.
func (h *headerCell) Tapped(*fyne.PointEvent) {
	if h.onTap != nil {
		h.onTap(h.col)
	}
}

// CreateRenderer implements fyne.Widget.
func (h *headerCell) CreateRenderer() fyne.WidgetRenderer {
	r := &headerCellRenderer{h: h}
	r.Refresh()
	return r
}

type headerCellRenderer struct{ h *headerCell }

func (r *headerCellRenderer) Layout(size fyne.Size) {
	h := r.h
	th := h.Theme()
	gap := th.Size(theme.SizeNamePadding)
	pad := th.Size(sizeCellPadding)
	ts := h.text.MinSize()
	h.text.Move(fyne.NewPos(pad, (size.Height-ts.Height)/2))
	h.text.Resize(ts)
	is := th.Size(sizeSortIcon)
	if h.menu || h.box {
		is = th.Size(sizeCellIcon)
		h.arrow.img.Move(fyne.NewPos((size.Width-is)/2, (size.Height-is)/2))
	} else {
		h.arrow.img.Move(fyne.NewPos(pad+ts.Width+gap/2, (size.Height-is)/2))
	}
	h.arrow.img.Resize(fyne.NewSize(is, is))
	h.sep.Move(fyne.NewPos(size.Width+gap/2-0.5, size.Height/4))
	h.sep.Resize(fyne.NewSize(1, size.Height/2))
	h.line.Move(fyne.NewPos(-gap/2, size.Height-1))
	h.line.Resize(fyne.NewSize(size.Width+gap, 1))
}

func (r *headerCellRenderer) MinSize() fyne.Size {
	th := r.h.Theme()
	h := fyne.MeasureText("Mg", th.Size(theme.SizeNameText), fyne.TextStyle{Bold: true}).Height
	return fyne.NewSize(2*th.Size(sizeCellPadding)+h, h+2*th.Size(sizeRowPadding))
}

func (r *headerCellRenderer) Refresh() {
	h := r.h
	th := h.Theme()
	h.text.Text = h.title
	// Muted bold text on the stage, a hairline under it and a short divider between the columns,
	// both in the bracket's ink: no band. The dividers show where a column is dragged wider (the
	// owner missed them in the first build of Dark Cockpit, 2026-10-21).
	h.text.TextSize = th.Size(theme.SizeNameText) - 1
	h.text.TextStyle = fyne.TextStyle{Bold: true}
	h.text.Color = themeColor(th, colorMuted)
	h.sep.FillColor = themeColor(th, colorBracket)
	if h.fixed {
		h.sep.FillColor = color.Transparent // a column that keeps its width has no divider to drag
	}
	h.line.FillColor = themeColor(th, colorBracket)
	var arrow fyne.Resource
	switch h.sort {
	case 1:
		arrow = theme.MenuDropUpIcon()
	case -1:
		arrow = theme.MenuDropDownIcon()
	}
	switch {
	case h.box:
		res, c := headerCheckIcon(h.boxed)
		h.arrow.set(th, res, c)
	case h.menu:
		h.arrow.set(th, theme.MoreVerticalIcon(), colorMuted)
	default:
		h.arrow.set(th, arrow, colorAccent)
	}
	r.Layout(h.Size())
	h.text.Refresh()
	h.sep.Refresh()
	h.line.Refresh()
}

func (r *headerCellRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.h.line, r.h.sep, r.h.text, r.h.arrow.img}
}

func (r *headerCellRenderer) Destroy() {}
