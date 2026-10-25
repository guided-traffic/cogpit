package engine

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/guided-traffic/cogpit/internal/kubeconfig"
)

// Bounds of a workspace file read back (Cogpit's defaults): what is beyond them is dropped on load
// without costing the rest, as the hotbar's and the preferences' files are read.
const (
	maxRestoredTabs  = 200  // tabs restored, in the bar's order
	maxStoredKey     = 512  // bytes of a view key, a column id, a namespace, a group or a sort column
	maxStoredList    = 1000 // namespaces or groups of a tab, column ids of a view
	maxStoredViews   = 1000 // views with a column layout
	maxStoredTitle   = 256  // bytes of a view's title
	maxStoredContext = maxHotbarContext
)

// storedText reports whether s may be kept: valid UTF-8 of at most n bytes.
func storedText(s string, n int) bool { return len(s) <= n && utf8.ValidString(s) }

// storedKeys keeps the non-empty, valid keys of a list, at most maxStoredList; nil for none.
func storedKeys(list []string) []string {
	var out []string
	for _, k := range list {
		if k != "" && storedText(k, maxStoredKey) && len(out) < maxStoredList {
			out = append(out, k)
		}
	}
	return out
}

// uniqueKeys drops the repeats of a list, keeping its order; nil for none.
func uniqueKeys(list []string) []string {
	var out []string
	for _, k := range list {
		if !slices.Contains(out, k) {
			out = append(out, k)
		}
	}
	return out
}

// storedStates keeps a tab's status filter: valid UTF-8 states, as ui.setStatusFilter takes them.
func storedStates(states []string) []string {
	var valid []string
	for _, s := range states {
		if utf8.ValidString(s) {
			valid = append(valid, s)
		}
	}
	return CleanStates(valid)
}

// storedHistory keeps a tab's history of valid view keys, the newest maxTabHistory.
func storedHistory(h []string) []string {
	out := storedKeys(h)
	if len(out) > maxTabHistory {
		out = out[len(out)-maxTabHistory:]
	}
	return out
}

// storedLayout keeps a column layout's valid views and ids; nil for none.
func storedLayout(m map[string][]string) map[string][]string {
	var out map[string][]string
	for view, ids := range m {
		if view == "" || !storedText(view, maxStoredKey) || len(out) >= maxStoredViews {
			continue
		}
		if ids = storedKeys(ids); ids == nil {
			continue
		}
		if out == nil {
			out = map[string][]string{}
		}
		out[view] = ids
	}
	return out
}

// normalize makes a document read from a file one the UI state can take: tabs with a valid context
// and view, at most maxRestoredTabs, one preview tab per cluster at most (D-117; a file written
// before it holds one in all), the fields within their bounds, and Active a tab's index or -1.
func (doc *workspaceDoc) normalize() {
	doc.Version = workspaceVersion
	tabs := make([]storedTab, 0, min(len(doc.Tabs), maxRestoredTabs))
	active, preview := -1, make(map[string]bool)
	for i, t := range doc.Tabs {
		if strings.TrimSpace(t.Context) == "" || !storedText(t.Context, maxStoredContext) ||
			!storedText(t.View, maxStoredKey) || len(tabs) == maxRestoredTabs {
			continue
		}
		if i == doc.Active {
			active = len(tabs)
		}
		t.Preview = t.Preview && !preview[t.Context]
		preview[t.Context] = preview[t.Context] || t.Preview
		if !storedText(t.ViewTitle, maxStoredTitle) {
			t.ViewTitle = ""
		}
		t.Namespaces, t.Groups = storedKeys(t.Namespaces), storedKeys(t.Groups)
		if !storedText(t.Filter, MaxFilterPattern) {
			t.Filter = ""
		}
		t.States = storedStates(t.States)
		if !storedText(t.Sort.Column, maxStoredKey) {
			t.Sort = SortSpec{}
		}
		t.Back, t.Forward = storedHistory(t.Back), storedHistory(t.Forward)
		t.Used = min(t.Used, uint64(maxRestoredTabs))
		tabs = append(tabs, t)
	}
	doc.Tabs, doc.Active = tabs, active
	doc.HiddenColumns, doc.ColumnOrder = storedLayout(doc.HiddenColumns), storedLayout(doc.ColumnOrder)
	doc.CollapsedSources = uniqueKeys(storedKeys(doc.CollapsedSources))
	doc.Connected = storedContexts(doc.Connected)
	if !slices.Contains(doc.Connected, doc.Shown) {
		doc.Shown = ""
	}
}

// storedContexts keeps the connections of a file read back: valid context ids without repeats, at
// most maxRestoredTabs; nil for none.
func storedContexts(ids []string) []string {
	var out []string
	for _, id := range ids {
		if strings.TrimSpace(id) == "" || !storedText(id, maxStoredContext) || slices.Contains(out, id) || len(out) == maxRestoredTabs {
			continue
		}
		out = append(out, id)
	}
	return out
}

// goneTabs counts the tabs of one context that were closed or not restored.
type goneTabs struct {
	Context string
	Tabs    int
}

func addGone(list []goneTabs, contextID string) []goneTabs {
	if i := slices.IndexFunc(list, func(g goneTabs) bool { return g.Context == contextID }); i >= 0 {
		list[i].Tabs++
		return list
	}
	return append(list, goneTabs{Context: contextID, Tabs: 1})
}

// restoreWorkspace puts a document into the UI state: its column layout and the window's layout,
// and, when tabs is set (the preference "Restore the last session", restore.go), its tabs but
// those of contexts that are certainly gone (goneContexts), with fresh ids and their titles. The
// tab shown last is marked active and its cluster becomes UIState.Context, Disconnected, as after
// cluster.disconnect: nothing connects here (RestoreLastSession does). It returns the dropped tabs.
func (s *Session) restoreWorkspace(doc workspaceDoc, tabs bool) []goneTabs {
	if !tabs {
		doc.Tabs, doc.Active = nil, -1 // a start without the last session's tabs (D-69 item 8)
	}
	ids := make([]string, 0, len(doc.Tabs))
	for _, t := range doc.Tabs {
		ids = append(ids, t.Context)
	}
	gone := s.goneContexts(ids, false)
	var (
		restored []ViewTab
		dropped  []goneTabs
		clock    uint64
	)
	active := -1
	for i, st := range doc.Tabs {
		if gone[st.Context] {
			dropped = addGone(dropped, st.Context)
			continue
		}
		if st.View != "" && st.ViewTitle != "" {
			s.NoteViewTitle(st.View, st.ViewTitle)
		}
		if i == doc.Active {
			active = len(restored)
		}
		clock = max(clock, st.Used)
		restored = append(restored, ViewTab{ID: "tab-" + strconv.Itoa(len(restored)+1), Context: st.Context, View: st.View,
			Preview: st.Preview, Namespaces: st.Namespaces, Groups: st.Groups, Filter: st.Filter,
			FilterOptions: st.FilterOptions, States: st.States, Sort: st.Sort, Back: placesOf(st.Back), Forward: placesOf(st.Forward), used: st.Used})
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	u := &s.ui
	for k := range restored {
		restored[k].Title = tabTitle(s.viewTitle, restored[k], u.ClusterSettings)
	}
	u.HiddenColumns, u.ColumnOrder = doc.HiddenColumns, doc.ColumnOrder
	u.Navigator.Collapsed = doc.CollapsedSources
	u.NavigatorHidden, u.Chat = doc.NavigatorHidden, doc.Chat // the layout (F7, layout.go)
	u.Tabs, u.tabSeq, u.tabClock = restored, uint64(len(restored)), clock
	if active >= 0 && u.Context == "" && u.Connection.State == Disconnected {
		u.ActiveTab, u.Context = restored[active].ID, restored[active].Context
		u.User = s.users.shown(u.Context, false) // its last known user (whoami.go)
	}
	return dropped
}

// goneContexts returns which of the context ids are certainly gone, never on a guess:
//   - a context of a kubeconfig source's file whose file does not exist any more (deleted, or moved
//     out of its sync folder);
//   - unless fileGoneOnly: one whose file still parses but no longer defines it, one whose source
//     was removed, and one the connector does not offer while its list of contexts read without an
//     error (the demo cluster, the merged kubeconfig of a run without a configuration directory).
//
// A file that cannot be read or parsed for a moment, or a list of sources that cannot be read,
// leaves its contexts alone. It reads the sources' files, never a cluster.
func (s *Session) goneContexts(ids []string, fileGoneOnly bool) map[string]bool {
	out := map[string]bool{}
	if len(ids) == 0 {
		return out
	}
	k := s.KubeconfigSources()
	var srcs []KubeconfigSource
	srcErr := errNoSources
	if k != nil {
		srcs, srcErr = k.List()
	}
	offered, parsed := map[string]bool{}, map[string]bool{}
	var listErr error
	if !fileGoneOnly {
		list, err := s.Contexts()
		listErr = err
		for _, c := range list {
			offered[c.Name] = true
		}
		if k != nil && srcErr == nil {
			files, _, _ := k.Files()
			for _, f := range files {
				if f.Err == nil {
					parsed[f.Label] = true
				}
			}
		}
	}
	for _, id := range ids {
		if id == "" || out[id] || offered[id] {
			continue
		}
		path, label, ok := sourceFileOf(srcs, id)
		switch {
		case srcErr == nil && ok:
			_, err := os.Stat(path)
			out[id] = errors.Is(err, fs.ErrNotExist) || (!fileGoneOnly && err == nil && parsed[label])
		case fileGoneOnly:
		case srcErr == nil && looksLikeSourceID(id):
			out[id] = true // no source covers its file any more
		default:
			out[id] = listErr == nil
		}
	}
	return out
}

// looksLikeSourceID reports whether id has the form of a source context's id, "<context> [<file>]".
func looksLikeSourceID(id string) bool {
	return strings.HasSuffix(id, "]") && strings.Contains(id, " [")
}

// sourceFileOf returns the file a source context id comes from and the file's label: the source's
// own file, or a file of a sync folder. ok is false when no source of srcs covers the id.
func sourceFileOf(srcs []KubeconfigSource, id string) (path, label string, ok bool) {
	for _, src := range srcs {
		if !ofSource(src, id) {
			continue
		}
		if src.Kind != SourceFolder {
			return src.Path, src.ID, true
		}
		rest := strings.TrimSuffix(id, "]")
		name := rest[strings.LastIndex(rest, " ["+src.ID)+len(" [")+len(src.ID):]
		return filepath.Join(src.Path, name), src.ID + name, true
	}
	return "", "", false
}

// dropContexts closes the tabs of the contexts gone reports, the active one too (no tab is active
// afterwards, as after closing a cluster's last tab), their dock tabs too (dropDockOf, counted with
// them), and forgets Context when it names one of them
// and no connection holds it, so that nothing points at a context that is gone: the navigator
// shows its section, and a failed connect's error goes with its context. It returns the closed
// tabs per context.
func (u *UIState) dropContexts(gone func(string) bool) []goneTabs {
	if i := u.activeIndex(); i >= 0 && gone(u.Tabs[i].Context) {
		u.ActiveTab = ""
		if u.Tabs[i].Context == u.Context {
			u.View, u.Filter = "", ""
			u.Selected, u.Drawer = nil, nil
		}
	}
	var closed []goneTabs
	u.Tabs = slices.DeleteFunc(u.Tabs, func(t ViewTab) bool {
		if gone(t.Context) {
			closed = addGone(closed, t.Context)
			return true
		}
		return false
	})
	u.forgetSelections(gone)            // the preview tab's remembered selections of them (selectionmemory.go)
	closed = u.dropDockOf(gone, closed) // and the dock's tabs of those contexts (dock_gone.go)
	if u.Context != "" && gone(u.Context) && !activeConnection(u.Connection.State) {
		u.Context = ""
		if u.Connection.State == ConnectError {
			u.Connection = Connection{State: Disconnected}
		}
	}
	return closed
}

// closeTabsOfGoneFiles closes the tabs of the contexts whose kubeconfig file is gone (deleted, or
// moved out of its sync folder) and forgets UIState.Context when it names one of them, with a
// notice naming the clusters (WatchKubeconfigs, after CloseGoneSourceConnection closed a connection
// made from such a file). A file that is only rewritten, or does not parse for a moment, closes
// nothing, as it closes no connection.
func (s *Session) closeTabsOfGoneFiles() {
	u := s.UIState()
	ids := make([]string, 0, len(u.Tabs)+len(u.Dock)+1)
	ids = append(ids, u.Context)
	for _, t := range u.Tabs {
		ids = append(ids, t.Context)
	}
	for _, t := range u.Dock { // the dock's tabs close with their context (dock_gone.go)
		ids = append(ids, t.Context)
	}
	gone := s.goneContexts(ids, true)
	if len(gone) == 0 || !slices.ContainsFunc(ids, func(id string) bool { return gone[id] }) {
		return
	}
	isGone := func(id string) bool { return gone[id] }
	kept := s.keepDraftsOf(isGone) // an editor's unsaved text stays as a draft
	var closed []goneTabs
	s.UpdateUI(func(u *UIState) { closed = u.dropContexts(isGone) })
	notice := closedTabsNotice(closed)
	if more := keptDraftsText(kept, notice == ""); more != "" {
		notice = strings.TrimSpace(notice + " " + more)
	}
	if notice != "" {
		s.emit(Event{Type: EventToast, Level: LevelInfo, Message: notice})
	}
}

// tabsOf counts tabs in words: "1 tab", "3 tabs".
func tabsOf(list []goneTabs) (string, int) {
	n := 0
	for _, g := range list {
		n += g.Tabs
	}
	if n == 1 {
		return "1 tab", n
	}
	return strconv.Itoa(n) + " tabs", n
}

// clusterNames names the contexts of list, at most three and how many more.
func clusterNames(list []goneTabs) string {
	names := make([]string, 0, 3)
	for _, g := range list[:min(len(list), 3)] {
		names = append(names, kubeconfig.DisplayText(g.Context, 200))
	}
	text := strings.Join(names, ", ")
	if more := len(list) - len(names); more > 0 {
		text += fmt.Sprintf(" and %d more", more)
	} else if len(names) > 1 {
		text = strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
	}
	return text
}

// notRestoredNotice says which tabs of the last run were not restored; "" for none.
func notRestoredNotice(dropped []goneTabs) string {
	if len(dropped) == 0 {
		return ""
	}
	count, n := tabsOf(dropped)
	verb := "was"
	if n > 1 {
		verb = "were"
	}
	return fmt.Sprintf("%s of the last run %s not restored: no kubeconfig source offers %s any more.", count, verb, clusterNames(dropped))
}

// closedTabsNotice says which tabs closed because their kubeconfig file is gone; "" for none.
func closedTabsNotice(closed []goneTabs) string {
	if len(closed) == 0 {
		return ""
	}
	count, _ := tabsOf(closed)
	whose := "its kubeconfig file is"
	if len(closed) > 1 {
		whose = "their kubeconfig files are"
	}
	return fmt.Sprintf("Closed %s of %s: %s gone.", count, clusterNames(closed), whose)
}
