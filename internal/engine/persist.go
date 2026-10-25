package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// What the user arranged in the window survives a restart (D-41, G5, G13): the tab bar's tabs and
// the tables' column layout are kept in WorkspaceFile under Cogpit's configuration directory,
// owner-only (0600 in a 0700 directory) and replaced atomically (writePrivateFile), written a
// moment after they stop changing (WorkspaceDelay, at least every workspaceMaxWait while they keep
// changing) and when the session closes.
//
// Since 2026-10-16 (D-69 item 8, restore.go) the tabs come back only while the preference "Restore
// the last session" is on, and then the clusters connected at quit connect again; off, Cogpit
// starts without tabs and connections. The file keeps both either way (connected, shown), so that
// turning the preference on restores the session of the last quit. A file written before has no
// connections: its tabs come back without a connect, as in v0.17.0.
//
// What it holds (D-41, night decision): per tab its cluster (the context id), its view and the
// view's title, whether it is its cluster's preview tab, its place in the bar, when it was last shown (a
// rank, so that a cluster connected from the navigator shows its most recently used tab), its
// namespace and API group filters, its search text with the search's toggles, its status filter
// (2026-10-14), its sort and its history (the places' view keys alone: a place's filters and
// drawer are the session's, 2026-10-25); which tab was shown last; and the hidden
// columns and the column order per view key, the same for every cluster, as UIState keeps them;
// since 2026-10-14 the navigator's collapsed source groups (A63); since 2026-10-15 whether the
// navigator is hidden and the chat column shown (F7). Never a selection or the drawer's object (they name objects that may be gone at the next start),
// never a dock tab (logs, terminals, editors: an editor's unsaved text has its draft, D-11), never
// a credential or an object's value.
//
// At start (attach, in New), with the preference on, the tabs show at once and nothing connects
// yet: the shown tab's cluster becomes UIState.Context, Disconnected, as after cluster.disconnect,
// so the tab is marked and the workspace shows the welcome page; the clusters of the last session
// connect once the app calls RestoreLastSession (restore.go), and a tab connects its cluster when
// the user shows it (SelectTab).
// The tabs of a context no kubeconfig source offers any more are dropped with a notice
// (restoreGoneTabs). A file that does not load is ignored, reported, and moved to
// <file>.unreadable before the first write, never overwritten (as the preferences' files).

// WorkspaceFile is the tabs' and the column layout's file under Cogpit's configuration directory.
const WorkspaceFile = "workspace.json"

const (
	// WorkspaceDelay is how long after the last change of the tabs or the layout they are written.
	WorkspaceDelay = time.Second
	// workspaceMaxWait bounds how long a layout that keeps changing goes unwritten.
	workspaceMaxWait = 5 * time.Second
	workspaceVersion = 1
)

// Workspace keeps a session's tabs and column layout across a restart. Create it with
// NewWorkspace and pass it as Options.Workspace; it belongs to one session. Two Cogpit processes
// keep one copy each and the last write wins, as with the hotbar.
type Workspace struct {
	dir     string // <user config dir>/Cogpit; empty: nothing is read or written
	delay   time.Duration
	maxWait time.Duration

	// wmu serialises the file's writes: a timer's and the flush at close. Taken before mu.
	wmu sync.Mutex

	mu         sync.Mutex
	s          *Session
	lastRev    uint64    // the newest UI state seen; older ones delivered late are skipped
	written    []byte    // the document as the file holds it (read at start, or last written)
	pending    []byte    // the document waiting for the timer; nil when none waits
	since      time.Time // when the first change not yet written came; zero when none waits
	timer      *time.Timer
	gen        uint64 // the newest schedule; an older timer's write is skipped
	unreadable bool   // the file did not load: it is moved aside before the first write
	closed     bool
	warned     bool // a failed write was logged

	// plan is what RestoreLastSession connects (restore.go), from the file read at start while the
	// preference is on; nil once taken. restoreCancel ends its connects when the session closes.
	plan          *restorePlan
	restoreCancel context.CancelFunc
}

// NewWorkspace returns the workspace kept in dir (<user config dir>/Cogpit); an empty dir keeps
// nothing. Nothing is read until a session attaches it.
func NewWorkspace(dir string) *Workspace {
	return &Workspace{dir: dir, delay: WorkspaceDelay, maxWait: workspaceMaxWait}
}

// WithDelay sets how long after the last change the file is written (WorkspaceDelay by default)
// and returns w; call it before passing w to New. Tests shorten it.
func (w *Workspace) WithDelay(delay time.Duration) *Workspace {
	w.delay = delay
	w.maxWait = max(workspaceMaxWait, 5*delay)
	return w
}

// Path returns the workspace's file; empty when nothing is kept.
func (w *Workspace) Path() string {
	if w == nil || w.dir == "" {
		return ""
	}
	return filepath.Join(w.dir, WorkspaceFile)
}

// Workspace returns the session's workspace, or nil when it keeps none.
func (s *Session) Workspace() *Workspace { return s.opts.Workspace }

// workspaceDoc is the file's document.
type workspaceDoc struct {
	Version int `json:"version"`
	// Tabs are the tab bar's tabs in their order.
	Tabs []storedTab `json:"tabs"`
	// Active is the index of the tab shown last: the active one, else the most recently shown;
	// -1 for none.
	Active        int                 `json:"active"`
	HiddenColumns map[string][]string `json:"hiddenColumns,omitempty"`
	ColumnOrder   map[string][]string `json:"columnOrder,omitempty"`
	// CollapsedSources are the Local Kubeconfigs section's collapsed source groups, by their
	// headings' labels (NavigatorState.Collapsed; A63, 2026-10-14).
	CollapsedSources []string `json:"collapsedSources,omitempty"`
	// NavigatorHidden and Chat are the window's layout (F7, layout.go): the navigator hidden and
	// the chat column shown. The dock's is not kept: its tabs are not restored, and a dock without
	// tabs is closed.
	NavigatorHidden bool `json:"navigatorHidden,omitempty"`
	Chat            bool `json:"chat,omitempty"`
	// Connected are the clusters connected (or connecting) when the file was written, by context
	// id, and Shown the one the window showed (D-69 item 8, 2026-10-16): what "Restore the last
	// session" connects again. Absent in a file of v0.17.0 and before; the format version stays 1,
	// and a Cogpit of then ignores the two fields.
	Connected []string `json:"connected,omitempty"`
	Shown     string   `json:"shown,omitempty"`
}

// storedTab is one tab as kept: a ViewTab without its id, selection and drawer.
type storedTab struct {
	Context string `json:"context"`
	View    string `json:"view"`
	// ViewTitle is the view's title, so that a restored tab of a custom resource is named as before
	// its cluster is connected.
	ViewTitle     string        `json:"viewTitle,omitempty"`
	Preview       bool          `json:"preview,omitempty"`
	Namespaces    []string      `json:"namespaces,omitempty"`
	Groups        []string      `json:"groups,omitempty"`
	Filter        string        `json:"filter,omitempty"`
	FilterOptions FilterOptions `json:"filterOptions,omitzero"`
	States        []string      `json:"states,omitempty"` // the status filter (statusfilter.go)
	Sort          SortSpec      `json:"sort,omitzero"`
	Back          []string      `json:"back,omitempty"`
	Forward       []string      `json:"forward,omitempty"`
	// Used ranks when the tab was last shown among the tabs, 1 the longest ago; 0: never shown.
	Used uint64 `json:"used,omitempty"`
}

// workspaceDocOf is what the file keeps of a UI state; viewTitle names a view when its title is
// known (Session.knownViewTitle).
func workspaceDocOf(u *UIState, viewTitle func(string) string) workspaceDoc {
	doc := workspaceDoc{Version: workspaceVersion, Tabs: make([]storedTab, len(u.Tabs)), Active: -1,
		HiddenColumns: u.HiddenColumns, ColumnOrder: u.ColumnOrder, CollapsedSources: u.Navigator.Collapsed,
		NavigatorHidden: u.NavigatorHidden, Chat: u.Chat}
	for _, c := range u.Connections { // connected, connecting, or being restored in the background
		doc.Connected = append(doc.Connected, c.Context)
	}
	if u.Context != "" && activeConnection(u.Connection.State) {
		doc.Shown = u.Context
	}
	ranks := usedRanks(u.Tabs)
	for i, t := range u.Tabs {
		doc.Tabs[i] = storedTab{Context: t.Context, View: t.View, Preview: t.Preview,
			Namespaces: t.Namespaces, Groups: t.Groups, Filter: t.Filter, FilterOptions: t.FilterOptions,
			States: t.States, Sort: t.Sort, Back: PlaceViews(t.Back), Forward: PlaceViews(t.Forward), Used: ranks[i]}
		if t.View != "" && viewTitle != nil {
			doc.Tabs[i].ViewTitle = viewTitle(t.View)
		}
	}
	if i := u.activeIndex(); i >= 0 {
		doc.Active = i
	} else if j := u.mostRecentTab(); j >= 0 {
		doc.Active = j
	}
	return doc
}

// knownViewTitle is a view's title when Cogpit knows it: a page's, or one remembered for a kind
// (NoteViewTitle); "" otherwise, so that the file never keeps viewTitle's guess from the key.
func (s *Session) knownViewTitle(view string) string {
	if t, ok := PageTitle(view); ok {
		return t
	}
	if v, ok := s.titles.Load(view); ok {
		return v.(string)
	}
	return ""
}

// usedRanks numbers the tabs by when they were last shown: 1 the longest ago, 0 never shown.
func usedRanks(tabs []ViewTab) []uint64 {
	ranks := make([]uint64, len(tabs))
	for i, t := range tabs {
		if t.used == 0 {
			continue
		}
		r := uint64(1)
		for _, o := range tabs {
			if o.used != 0 && o.used < t.used {
				r++
			}
		}
		ranks[i] = r
	}
	return ranks
}

// mostRecentTab returns the index of the most recently shown tab, or -1 when none was shown.
func (u *UIState) mostRecentTab() int {
	best := -1
	for i, t := range u.Tabs {
		if t.used > 0 && (best < 0 || t.used > u.Tabs[best].used) {
			best = i
		}
	}
	return best
}

func marshalWorkspace(doc workspaceDoc) ([]byte, error) {
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// load reads the file. A missing file is an empty workspace; one that does not load is an error,
// and w.unreadable is set so that the first write keeps it aside.
func (w *Workspace) load() (workspaceDoc, error) {
	doc, empty := workspaceDoc{Active: -1}, workspaceDoc{Version: workspaceVersion, Tabs: []storedTab{}, Active: -1}
	path := w.Path()
	if path == "" {
		return empty, nil
	}
	if err := readPrivateJSON(path, maxPrivateDoc, &doc); err != nil {
		w.unreadable = true
		return empty, fmt.Errorf("the tabs of the last run were not restored: %s is unreadable (%v); "+
			"Cogpit keeps it as %s.unreadable at the first change", path, err, WorkspaceFile)
	}
	if doc.Version != 0 && doc.Version != workspaceVersion {
		w.unreadable = true
		return empty, fmt.Errorf("the tabs of the last run were not restored: %s has format version %d, "+
			"this Cogpit reads %d; it keeps the file as %s.unreadable at the first change", path, doc.Version, workspaceVersion, WorkspaceFile)
	}
	doc.normalize()
	return doc, nil
}

// attach restores the tabs and the column layout into the session's UI state and follows its
// changes. It is nil-safe. Nothing is connected and no cluster is asked; the kubeconfig sources'
// files are read to drop the tabs of contexts that are gone.
func (w *Workspace) attach(s *Session) {
	if w == nil {
		return
	}
	doc, err := w.load()
	if err != nil {
		s.log.Warn("workspace", "err", err)
	}
	read, _ := marshalWorkspace(doc)
	restore := s.restoreSession() // the preference "Restore the last session" (restore.go)
	dropped := s.restoreWorkspace(doc, restore)
	var plan *restorePlan
	var notConnected []string
	if restore {
		plan, notConnected = s.planRestore(doc)
	}
	s.mu.Lock()
	now := workspaceDocOf(&s.ui, s.knownViewTitle)
	rev := s.ui.Revision
	s.mu.Unlock()
	data, _ := marshalWorkspace(now)

	w.mu.Lock()
	w.s, w.lastRev, w.plan = s, rev, plan
	if !w.unreadable {
		w.written = read
		if !bytes.Equal(data, read) {
			w.scheduleLocked(data) // the file without the tabs that were dropped
		}
	}
	w.mu.Unlock()

	if err != nil {
		msg := err.Error()
		s.emit(Event{Type: EventToast, Level: LevelWarning, Message: strings.ToUpper(msg[:1]) + msg[1:]})
	}
	if notice := notRestoredNotice(dropped); notice != "" {
		s.emit(Event{Type: EventToast, Level: LevelInfo, Message: notice})
	}
	if notice := notReconnectedNotice(notConnected); notice != "" {
		s.emit(Event{Type: EventToast, Level: LevelInfo, Message: notice})
	}
	s.Subscribe(w.onEvent)
}

// takePlan hands the restore's plan over once, with the cancel of its connects, which close calls;
// nil without one. Nil-safe.
func (w *Workspace) takePlan(cancel context.CancelFunc) *restorePlan {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	plan := w.plan
	w.plan = nil
	if plan != nil && !w.closed {
		w.restoreCancel = cancel
		return plan
	}
	return nil
}

// onEvent runs on the goroutine that emitted the event: it must not block.
func (w *Workspace) onEvent(ev Event) {
	if ev.Type != EventUIState || ev.UIState == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed || w.s == nil || ev.UIState.Revision <= w.lastRev {
		return
	}
	w.lastRev = ev.UIState.Revision
	data, err := marshalWorkspace(workspaceDocOf(ev.UIState, w.s.knownViewTitle))
	if err != nil || bytes.Equal(data, w.pending) {
		return
	}
	if bytes.Equal(data, w.written) { // changed back before it was written: nothing to write
		w.pending, w.since = nil, time.Time{}
		w.gen++
		if w.timer != nil {
			w.timer.Stop()
			w.timer = nil
		}
		return
	}
	w.scheduleLocked(data)
}

// scheduleLocked (re)starts the timer that writes data. w.mu is held.
func (w *Workspace) scheduleLocked(data []byte) {
	now := time.Now()
	if w.pending == nil {
		w.since = now
	}
	w.pending = data
	wait := w.delay
	if rest := w.since.Add(w.maxWait).Sub(now); rest < wait {
		wait = max(rest, 0)
	}
	if w.timer != nil {
		w.timer.Stop()
	}
	w.gen++
	gen, s := w.gen, w.s
	w.timer = time.AfterFunc(wait, func() {
		defer s.recoverTo("keeping the tabs and the column layout")
		w.flush(gen)
	})
}

// flush writes the waiting document if gen is still the newest schedule.
func (w *Workspace) flush(gen uint64) {
	w.wmu.Lock()
	defer w.wmu.Unlock()
	w.mu.Lock()
	if gen != w.gen || w.pending == nil {
		w.mu.Unlock()
		return
	}
	data := w.pending
	w.pending, w.since, w.timer = nil, time.Time{}, nil
	unreadable := w.unreadable
	w.mu.Unlock()

	err := w.write(data, unreadable)
	w.mu.Lock()
	defer w.mu.Unlock()
	if err != nil {
		if !w.warned && w.s != nil {
			w.s.log.Warn("the tabs and the column layout could not be kept", "file", w.Path(), "err", err)
		}
		w.warned = true
		return
	}
	w.written, w.unreadable = data, false
}

// write replaces the file with data, owner-only and atomically; an unreadable file is first moved
// aside to <file>.unreadable.
func (w *Workspace) write(data []byte, unreadable bool) error {
	path := w.Path()
	if path == "" {
		return nil
	}
	if unreadable {
		if err := renameFile(path, path+".unreadable"); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("keeping the unreadable file aside: %w", err)
		}
	}
	return writePrivateFile(path, data)
}

// close writes what waits and stops following the session; nil-safe. Session.Close calls it
// before the connection closes, so the file keeps the tabs as the user left them.
func (w *Workspace) close() {
	if w == nil {
		return
	}
	w.mu.Lock()
	w.closed = true
	if w.timer != nil {
		w.timer.Stop()
	}
	gen := w.gen
	cancel := w.restoreCancel
	w.mu.Unlock()
	w.flush(gen)
	if cancel != nil {
		cancel() // the restore's connects still under way give up (restore.go)
	}
}
