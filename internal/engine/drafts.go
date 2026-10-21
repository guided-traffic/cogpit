package engine

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/guided-traffic/cogpit/internal/model"
)

// Drafts keep the unsaved text of Edit and Create resource tabs on disk, so that a crash or a quit
// does not lose it (D-11, G9). A tab's draft is written a moment after its text stops changing
// (DraftDelay, and at least every draftMaxWait while it keeps changing) into
// <user config dir>/Cogpit/drafts/<id>.json, owner-only and atomically (writePrivateFile), and is
// removed when the text is saved again (an Edit tab's text is the object it is based on, a Create
// tab's is the text it opened with or the one it created in full) or when the tab is closed. A
// quit flushes the drafts that are waiting. When a context is connected, the drafts of that
// context that no open tab and no other live Cogpit process owns are offered (EventDrafts):
// editor.restoreDraft opens the tab with the text again, editor.discardDraft deletes it. A Config
// Map's values edited in the drawer are kept the same way (drafts_drawer.go).
//
// Never for values (D-19, owner 2026-10-12): an Edit tab of a Secret, a text that holds a Secret's
// manifest or might be one being typed, and the Helm Upgrade and Install tabs, whose values can
// hold credentials, have no draft; a draft that became one is removed. Every other kind's text is
// kept as typed, as the user's own unsaved work (docs/security/credentials.md).
//
// Each draft names the Cogpit process that wrote it (JournalOwner, the drain and Helm journals'
// owners, H-22): a draft whose owner still runs belongs to a tab open in that process and is not
// offered elsewhere.

// ErrNoDrafts disables the draft commands of a session that keeps no drafts (a headless one).
var ErrNoDrafts = errors.New("this Cogpit session keeps no drafts of unsaved editor text")

// ErrDraftNotFound is returned for a draft id that names no offered draft.
var ErrDraftNotFound = errors.New("no such draft of unsaved editor text")

// Timings and bounds of drafts.
const (
	// DraftDelay is how long after the last change of a tab's text its draft is written.
	DraftDelay = 2 * time.Second
	// draftMaxWait bounds how long a text that keeps changing goes without a draft.
	draftMaxWait = 10 * time.Second
	// DraftsDir is the drafts' folder in Cogpit's configuration directory.
	DraftsDir   = "drafts"
	draftSuffix = ".json"
	// maxDraftFileBytes bounds a draft file read back: the editor's largest text, JSON-escaped.
	maxDraftFileBytes = 6*MaxManifestBytes + 64<<10
	// maxDraftFiles bounds the files one listing reads.
	maxDraftFiles = 500
	draftVersion  = 1
)

// Draft is one draft file: the tab it was written for and its text.
type Draft struct {
	Version int      `json:"version"`
	ID      string   `json:"id"` // 32 hex digits, the file's name
	Type    DockType `json:"type"`
	Context string   `json:"context"`
	Title   string   `json:"title"`
	// Edit tabs: the object, and the version the text is based on, which a restored tab is based
	// on again: an apply then never overwrites a change the user has not seen (editor.go).
	Ref                 *model.ObjectRef `json:"ref,omitempty"`
	APIVersion          string           `json:"apiVersion,omitempty"`
	Kind                string           `json:"kind,omitempty"`
	BaseResourceVersion string           `json:"baseResourceVersion,omitempty"`
	BaseUID             string           `json:"baseUID,omitempty"`
	Saved               time.Time        `json:"saved"`
	Owner               *JournalOwner    `json:"owner,omitempty"`
	Text                string           `json:"text"`
	// A Config Map's values edited in the drawer (DraftConfigMapData, drafts_drawer.go): each
	// edited key's text and the value it is based on, the keys added with their text, the keys
	// removed.
	Values map[string]string `json:"values,omitempty"`
	From   map[string]string `json:"from,omitempty"`
	Add    map[string]string `json:"add,omitempty"`
	Remove []string          `json:"remove,omitempty"`
}

// DraftInfo is a draft as editor.drafts lists it: what it is for, never its text (restore it and
// read the tab with editor.get, whose rules apply).
type DraftInfo struct {
	ID                  string           `json:"id"`
	Type                DockType         `json:"type"`
	Context             string           `json:"context"`
	Title               string           `json:"title"`
	Ref                 *model.ObjectRef `json:"ref,omitempty"`
	BaseResourceVersion string           `json:"baseResourceVersion,omitempty"`
	Saved               time.Time        `json:"saved"`
	Lines               int              `json:"lines"`
	Bytes               int              `json:"bytes"`
	// Keys are the edited, added and removed keys of a Config Map's values kept from the drawer
	// (type configMapData), KeysRemoved those removed; Bytes is then the edited and added values'
	// size.
	Keys        []string `json:"keys,omitempty"`
	KeysRemoved []string `json:"keysRemoved,omitempty"`
}

func (dr Draft) info() DraftInfo {
	lines := strings.Count(dr.Text, "\n")
	if dr.Text != "" && !strings.HasSuffix(dr.Text, "\n") {
		lines++
	}
	info := DraftInfo{ID: dr.ID, Type: dr.Type, Context: dr.Context, Title: dr.Title, Ref: dr.Ref,
		BaseResourceVersion: dr.BaseResourceVersion, Saved: dr.Saved, Lines: lines, Bytes: len(dr.Text)}
	if dr.Type == DraftConfigMapData {
		return drawerDraftInfo(dr, info)
	}
	return info
}

// what names a draft in messages: "ConfigMap demo (namespace shop)", "a Create resource tab".
func (dr Draft) what() string {
	if dr.Type == DockCreate {
		return "the Create resource tab"
	}
	if dr.Ref != nil && dr.Ref.Namespace != "" {
		return dr.Title + " (namespace " + dr.Ref.Namespace + ")"
	}
	return dr.Title
}

// kept says what a draft keeps: "the unsaved text of Service web (namespace shop)", "the unsaved
// values of ConfigMap app (namespace shop)" for a drawer's.
func (dr Draft) kept() string {
	if dr.Type == DraftConfigMapData {
		return "the unsaved values of " + dr.what()
	}
	return "the unsaved text of " + dr.what()
}

// Drafts writes and offers a session's drafts. Create it with NewDrafts and pass it as
// Options.Drafts; it belongs to one session.
type Drafts struct {
	dir     string      // <cogpit>/drafts
	jd      *journalDir // <cogpit>: the process's owner (journallock.go)
	delay   time.Duration
	maxWait time.Duration
	now     func() time.Time

	// wmu serialises the draft files' writes, removals, restores and discards, so that a write
	// for a tab that was closed meanwhile cannot leave its file behind. Taken before mu; the
	// session's own locks (UI state, editors) may be taken under it, never the other way round:
	// events, which run under nothing, take mu only.
	wmu sync.Mutex

	mu       sync.Mutex
	s        *Session
	tabs     map[string]*draftTab // editor tab id → its draft
	owner    *JournalOwner        // registered on the first write
	lastRev  uint64
	lastDock map[string]bool // the Edit and Create tabs of the last UI state
	lastConn ConnectionState
	lastCtx  string
	closed   bool
	warned   bool
}

// draftTab is the draft of one open editor tab.
type draftTab struct {
	id    string // the draft's id, "" until the first write
	timer *time.Timer
	gen   uint64    // the newest schedule; an older timer's write is skipped
	since time.Time // the first change not yet written; zero when none waits
	gone  bool      // the tab was closed: nothing is written for it any more
	kept  bool      // the tab closes without the user (keep): its draft stays when it closes
}

// NewDrafts returns the drafts of a Cogpit configuration directory (<user config dir>/Cogpit):
// the files live in its drafts folder.
func NewDrafts(cogpitDir string) *Drafts { return newDrafts(cogpitDir, sharedJournalDir(cogpitDir)) }

func newDrafts(cogpitDir string, jd *journalDir) *Drafts {
	return &Drafts{dir: filepath.Join(cogpitDir, DraftsDir), jd: jd, delay: DraftDelay, maxWait: draftMaxWait,
		now: time.Now, tabs: map[string]*draftTab{}, lastDock: map[string]bool{}}
}

// WithDelay sets how long after the last change of a text its draft is written (DraftDelay by
// default) and returns d; call it before passing d to New. Tests shorten it.
func (d *Drafts) WithDelay(delay time.Duration) *Drafts {
	d.delay = delay
	d.maxWait = max(draftMaxWait, 5*delay)
	return d
}

// Drafts returns the session's drafts, or nil when it keeps none.
func (s *Session) Drafts() *Drafts { return s.opts.Drafts }

// attach binds the drafts to their session and follows its editors, dock and connections. It is
// nil-safe.
func (d *Drafts) attach(s *Session) {
	if d == nil {
		return
	}
	d.mu.Lock()
	d.s = s
	d.mu.Unlock()
	s.Subscribe(d.onEvent)
}

// onEvent runs on the goroutine that emitted the event: it must not block.
func (d *Drafts) onEvent(ev Event) {
	switch ev.Type {
	case EventEditor:
		d.schedule(ev.View)
	case EventUIState:
		if ev.UIState != nil {
			d.uiChanged(ev.UIState)
		}
	}
}

// schedule (re)starts the timer that writes a tab's draft after its text stopped changing.
func (d *Drafts) schedule(tabID string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed || d.s == nil {
		return
	}
	t := d.tabs[tabID]
	if t == nil {
		t = &draftTab{}
		d.tabs[tabID] = t
	}
	now := d.now()
	if t.since.IsZero() {
		t.since = now
	}
	wait := d.delay
	if rest := t.since.Add(d.maxWait).Sub(now); rest < wait {
		wait = max(rest, 0)
	}
	if t.timer != nil {
		t.timer.Stop()
	}
	t.gen++
	gen, s := t.gen, d.s
	t.timer = time.AfterFunc(wait, func() {
		defer s.recoverTo("keeping a draft of unsaved editor text")
		d.flush(tabID, gen)
	})
}

// uiChanged forgets the drafts of closed tabs and offers a newly connected context's drafts.
func (d *Drafts) uiChanged(u *UIState) {
	d.mu.Lock()
	if u.Revision <= d.lastRev || d.s == nil { // an older state delivered late
		d.mu.Unlock()
		return
	}
	d.lastRev = u.Revision
	open := map[string]bool{}
	for _, t := range u.Dock {
		if t.Type == DockEdit || t.Type == DockCreate {
			open[t.ID] = true
		}
	}
	var gone []string
	for id := range d.lastDock {
		if open[id] {
			continue
		}
		if t := d.tabs[id]; t != nil {
			t.gone = true
			if t.timer != nil {
				t.timer.Stop()
				t.timer = nil
			}
			if t.id != "" && !t.kept { // a kept draft stays, offered when its context connects (keep)
				gone = append(gone, t.id)
			}
			delete(d.tabs, id)
		}
	}
	d.lastDock = open
	prevConn, prevCtx := d.lastConn, d.lastCtx
	d.lastConn, d.lastCtx = u.Connection.State.asConnected(), u.Context // an interruption is no new connect (D-58)
	s := d.s
	d.mu.Unlock()

	if len(gone) > 0 { // closing a tab discards its text, and its draft with it (Cogpit's default)
		s.goSafe("removing a closed tab's draft", func() {
			d.wmu.Lock()
			defer d.wmu.Unlock()
			for _, id := range gone {
				d.removeFile(id)
			}
		})
	}
	if u.Connection.State.asConnected() == prevConn && u.Context == prevCtx {
		return
	}
	switch {
	case u.Connection.State == Connected && !u.Connection.Demo:
		ctxName := u.Context
		s.goSafe("offering unsaved editor drafts", func() { d.offer(s, ctxName) })
	case prevConn == Connected:
		d.changed("") // the previous cluster's drafts no longer apply
	}
}

// offer announces the drafts of a freshly connected context.
func (d *Drafts) offer(s *Session, ctxName string) {
	list, err := d.offered(ctxName, false)
	if err != nil {
		s.log.Error("engine: the drafts of unsaved editor text are unreadable", "err", err)
		d.changed("")
		return
	}
	msg := ""
	switch n := len(list); {
	case n == 1:
		msg = fmt.Sprintf("Cogpit kept %s in context %s", list[0].kept(), ctxName)
	case n > 1:
		msg = fmt.Sprintf("Cogpit kept the unsaved text of %d editor tabs in context %s", n, ctxName)
	}
	if b := s.Backend(); b != nil && b.Context == ctxName {
		d.changed(msg)
	}
}

// changed emits EventDrafts.
func (d *Drafts) changed(msg string) {
	d.mu.Lock()
	s := d.s
	d.mu.Unlock()
	if s != nil {
		s.emit(Event{Type: EventDrafts, Message: msg})
	}
}

// flush writes, or removes, a tab's draft as its editor holds it now. gen is the schedule it
// belongs to; a newer schedule's write supersedes it.
func (d *Drafts) flush(tabID string, gen uint64) {
	d.wmu.Lock()
	defer d.wmu.Unlock()
	d.mu.Lock()
	t, s := d.tabs[tabID], d.s
	if t == nil || t.gone || t.gen != gen || s == nil {
		d.mu.Unlock()
		return
	}
	t.timer, t.since = nil, time.Time{}
	d.mu.Unlock()
	d.flushLocked(s, tabID, t)
}

// flushLocked writes or removes a tab's draft, or a drawer's (drafts_drawer.go); wmu is held.
func (d *Drafts) flushLocked(s *Session, tabID string, t *draftTab) {
	if strings.HasPrefix(tabID, drawerDraftPrefix) {
		d.flushDrawerLocked(s, tabID, t)
		return
	}
	d.mu.Lock()
	id := t.id
	d.mu.Unlock()
	e, err := s.editorOf(tabID)
	if err != nil { // the tab closed before its draft was written, or never reached the dock
		d.mu.Lock()
		if d.tabs[tabID] == t {
			delete(d.tabs, tabID)
		}
		d.mu.Unlock()
		if id != "" {
			d.removeFile(id)
		}
		return
	}
	if !draftable(e) {
		if id != "" { // saved again, or it holds a Secret now: no draft
			d.removeFile(id)
		}
		return
	}
	if id == "" {
		if id, err = newDraftID(); err != nil {
			d.warn(err)
			return
		}
		d.mu.Lock()
		if t.gone {
			d.mu.Unlock()
			return
		}
		t.id = id
		d.mu.Unlock()
	}
	d.write(d.draftOf(e, id))
}

// keep writes the drafts of editor tabs that are about to close without the user (their context
// left Cogpit, dock_gone.go) at once, as their editors hold them, and keeps the files when the
// tabs close, where closing a tab discards its draft: the text is the user's unsaved work (D-11),
// offered when the context connects again. A tab with nothing unsaved, or a Secret's (D-19), keeps
// nothing. It returns how many drafts it kept. It is nil-safe.
func (d *Drafts) keep(s *Session, tabIDs []string) int {
	if d == nil || len(tabIDs) == 0 {
		return 0
	}
	d.wmu.Lock()
	defer d.wmu.Unlock()
	n := 0
	for _, tabID := range tabIDs {
		e, err := s.editorOf(tabID)
		if err != nil || !draftable(e) {
			continue
		}
		d.mu.Lock()
		if d.closed {
			d.mu.Unlock()
			return n // quitting: close wrote what waited
		}
		t := d.tabs[tabID]
		if t == nil {
			t = &draftTab{}
			d.tabs[tabID] = t
		}
		if t.timer != nil {
			t.timer.Stop()
			t.timer = nil
		}
		t.gen++ // a timer that fired meanwhile writes nothing
		t.since, t.kept = time.Time{}, true
		id := t.id
		d.mu.Unlock()
		if id == "" {
			if id, err = newDraftID(); err != nil {
				d.warn(err)
				continue
			}
			d.mu.Lock()
			t.id = id
			d.mu.Unlock()
		}
		d.write(d.draftOf(e, id))
		n++
	}
	return n
}

// close writes the drafts that wait for their timer, so that a quit keeps the newest text, and
// stops further writes. It is nil-safe.
func (d *Drafts) close() {
	if d == nil {
		return
	}
	type waiting struct {
		tab string
		gen uint64
	}
	var list []waiting
	d.mu.Lock()
	d.closed = true
	for id, t := range d.tabs {
		if t.timer != nil {
			t.timer.Stop()
			list = append(list, waiting{id, t.gen})
		}
	}
	d.mu.Unlock()
	for _, w := range list {
		d.flush(w.tab, w.gen)
	}
}

// draftable reports whether an editor's text is kept as a draft: an Edit or Create resource tab
// whose text is not saved, and never a Secret's (D-19).
func draftable(e editor) bool {
	var clean string
	switch e.tab.Type {
	case DockEdit:
		if RedactedKind(e.target.APIVersion, e.target.Kind) {
			return false
		}
		clean = e.baseText
	case DockCreate:
		clean = e.startText
	default: // Helm values can hold credentials: no drafts (helmupgrade.go, helminstall.go)
		return false
	}
	return e.text != clean && !textMayHoldSecret(e.text)
}

// mayBeSecretKind finds a kind that is Secret or SecretList, or one being typed that could still
// become either ("kind: Sec"), in any case, also in a list item or a flow mapping.
var mayBeSecretKind = regexp.MustCompile(`(?i)\bkind["']?\s*:\s*["']?s(e(c(r(e(t(l(i(s(t)?)?)?)?)?)?)?)?)?\b`)

// textMayHoldSecret reports whether a text holds a Secret's manifest or might while it is being
// typed: stricter than TextHoldsSecret, since a draft that holds values is on disk.
func textMayHoldSecret(text string) bool {
	return TextHoldsSecret(text) || mayBeSecretKind.MatchString(text)
}

// draftOf is the draft file of an editor.
func (d *Drafts) draftOf(e editor, id string) Draft {
	dr := Draft{Version: draftVersion, ID: id, Type: e.tab.Type, Context: e.tab.Context, Title: e.tab.Title,
		Saved: d.now().UTC(), Owner: d.self(), Text: e.text}
	if e.tab.Type == DockEdit {
		r := e.target.Ref
		dr.Ref = &r
		dr.APIVersion, dr.Kind = e.target.APIVersion, e.target.Kind
		dr.BaseResourceVersion, dr.BaseUID = e.baseRV, e.baseUID
	}
	return dr
}

// self returns the process's owner, registering it on the first call; nil when it cannot be
// registered, and the draft is written without one (offered to any process).
func (d *Drafts) self() *JournalOwner {
	d.mu.Lock()
	o := d.owner
	d.mu.Unlock()
	if o != nil {
		return o
	}
	err := d.jd.locked(true, func() error {
		var err error
		o, err = d.jd.self()
		return err
	})
	if err != nil {
		d.logger().Warn("engine: a draft is kept without naming its Cogpit process", "err", err)
		return nil
	}
	d.mu.Lock()
	d.owner = o
	d.mu.Unlock()
	return o
}

func (d *Drafts) logger() interface{ Warn(string, ...any) } {
	d.mu.Lock()
	s := d.s
	d.mu.Unlock()
	if s == nil {
		return nopWarner{}
	}
	return s.log
}

type nopWarner struct{}

func (nopWarner) Warn(string, ...any) {}

// path is a draft's file.
func (d *Drafts) path(id string) string { return filepath.Join(d.dir, id+draftSuffix) }

// write stores a draft; wmu is held. A failure is logged, and shown once per session.
func (d *Drafts) write(dr Draft) {
	data, err := json.Marshal(dr)
	if err == nil {
		err = writePrivateFile(d.path(dr.ID), data)
	}
	if err != nil {
		d.warn(err)
	}
}

// removeFile deletes a draft's file; wmu is held, or the draft is no open tab's (a Secret's file
// found by a listing). A missing file is no error.
func (d *Drafts) removeFile(id string) {
	if !validOwnerID(id) {
		return
	}
	if err := retryHeld(func() error { return os.Remove(d.path(id)) }); err != nil && !errors.Is(err, fs.ErrNotExist) {
		d.logger().Warn("engine: a draft of unsaved editor text could not be removed", "err", err)
	}
}

// warn logs a failed draft write and tells the user once.
func (d *Drafts) warn(err error) {
	d.mu.Lock()
	s, first := d.s, !d.warned
	d.warned = true
	d.mu.Unlock()
	if s == nil {
		return
	}
	s.log.Warn("engine: a draft of unsaved editor text could not be kept", "err", err)
	if first {
		s.emit(Event{Type: EventToast, Level: LevelWarning, Message: "Cogpit cannot keep drafts of unsaved editor text, so a crash loses it: " + err.Error()})
	}
}

// newDraftID returns a fresh draft id: 128 random bits as 32 hex digits, never reused.
func newDraftID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}

// readAll reads the draft files, oldest first. Files that are not drafts are skipped; a draft
// that holds a Secret's values, which Cogpit never writes, is removed.
func (d *Drafts) readAll() ([]Draft, error) {
	entries, err := os.ReadDir(d.dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Draft
	for _, e := range entries {
		if len(out) >= maxDraftFiles {
			break
		}
		id, ok := strings.CutSuffix(e.Name(), draftSuffix)
		if !ok || !validOwnerID(id) || !e.Type().IsRegular() {
			continue
		}
		dr, err := d.read(id)
		if err != nil {
			d.logger().Warn("engine: skipped a draft file", "file", e.Name(), "err", err)
			continue
		}
		out = append(out, dr)
	}
	slices.SortStableFunc(out, func(a, b Draft) int { return a.Saved.Compare(b.Saved) })
	return out, nil
}

// read reads and checks one draft file.
func (d *Drafts) read(id string) (Draft, error) {
	var dr Draft
	if !validOwnerID(id) {
		return dr, ErrDraftNotFound
	}
	path := d.path(id)
	if _, err := os.Lstat(path); errors.Is(err, fs.ErrNotExist) {
		return dr, fmt.Errorf("%w: %s", ErrDraftNotFound, id)
	}
	if err := readPrivateJSON(path, maxDraftFileBytes, &dr); err != nil {
		return dr, err
	}
	switch {
	case dr.ID != id:
		return dr, fmt.Errorf("the draft file %s names another id", id)
	case dr.Type != DockEdit && dr.Type != DockCreate && dr.Type != DraftConfigMapData:
		return dr, fmt.Errorf("the draft %s is of an unknown tab type %q", id, dr.Type)
	case dr.Context == "":
		return dr, fmt.Errorf("the draft %s names no context", id)
	case dr.Type == DockEdit && (dr.Ref == nil || dr.Ref.Name == "" || dr.Ref.Kind == ""):
		return dr, fmt.Errorf("the draft %s names no object", id)
	case len(dr.Text) > MaxManifestBytes:
		return dr, fmt.Errorf("the draft %s is larger than %s", id, model.FormatBytes(MaxManifestBytes))
	case dr.Type == DraftConfigMapData:
		if err := checkDrawerDraft(dr); err != nil {
			return dr, err
		}
	}
	if dr.Ref != nil {
		dr.Ref.Context = dr.Context
	}
	if draftHoldsSecret(dr) {
		d.removeFile(id) // never on disk (D-19), whoever wrote it
		return dr, fmt.Errorf("the draft %s held a Secret's values and was removed", id)
	}
	return dr, nil
}

// draftHoldsSecret reports whether a draft read back is a Secret's or might hold one.
func draftHoldsSecret(dr Draft) bool {
	if dr.Type == DockEdit && (RedactedKind(dr.APIVersion, dr.Kind) || dr.Ref.Kind == "secrets") {
		return true
	}
	return textMayHoldSecret(dr.Text)
}

// openDraftIDs are the ids of the drafts this session's open tabs own.
func (d *Drafts) openDraftIDs() map[string]bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	ids := map[string]bool{}
	for _, t := range d.tabs {
		if t.id != "" {
			ids[t.id] = true
		}
	}
	return ids
}

// offered returns the drafts of a context (all: of every context) that no open tab of this
// session and no other live Cogpit process owns, oldest first.
func (d *Drafts) offered(ctxName string, all bool) ([]Draft, error) {
	drafts, err := d.readAll()
	if err != nil || len(drafts) == 0 {
		return nil, err
	}
	open := d.openDraftIDs()
	var out []Draft
	err = d.jd.locked(false, func() error {
		for _, dr := range drafts {
			if open[dr.ID] || (!all && dr.Context != ctxName) || d.jd.aliveElsewhere(dr.Owner) {
				continue
			}
			out = append(out, dr)
		}
		return nil
	})
	return out, err
}

// offeredDraft reads one draft and checks that it is offered: no open tab of this session and no
// other live Cogpit process owns it. wmu is held.
func (d *Drafts) offeredDraft(id string) (Draft, error) {
	dr, err := d.read(id)
	if err != nil {
		return dr, err
	}
	if d.openDraftIDs()[id] {
		return dr, fmt.Errorf("the draft %s is the unsaved text of a tab open in this window", id)
	}
	var elsewhere *JournalOwner
	if err := d.jd.locked(false, func() error {
		if d.jd.aliveElsewhere(dr.Owner) {
			elsewhere = dr.Owner
		}
		return nil
	}); err != nil {
		return dr, err
	}
	if elsewhere != nil {
		return dr, fmt.Errorf("the draft of %s belongs to a tab open in another Cogpit window (process %d)", dr.what(), elsewhere.PID)
	}
	return dr, nil
}

// adopt makes a restored draft the draft of the tab that shows it and rewrites it at once, naming
// this process; wmu is held. A draft the tab had before is removed.
func (d *Drafts) adopt(s *Session, tabID string, dr Draft) {
	d.mu.Lock()
	t := d.tabs[tabID]
	old := ""
	if t == nil {
		t = &draftTab{}
		d.tabs[tabID] = t
	} else {
		old = t.id
		if t.timer != nil {
			t.timer.Stop()
			t.timer = nil
		}
		t.gen++
		t.since = time.Time{}
	}
	t.id = dr.ID
	d.lastDock[tabID] = true // the dock holds it; a close before the next UI state still removes it
	d.mu.Unlock()
	if old != "" && old != dr.ID {
		d.removeFile(old)
	}
	d.flushLocked(s, tabID, t)
}

// EditorDrafts lists the drafts Cogpit offers to restore: the connected context's (all: every
// context's), oldest first, without their text.
func (s *Session) EditorDrafts(all bool) ([]DraftInfo, error) {
	d := s.opts.Drafts
	if d == nil {
		return nil, ErrNoDrafts
	}
	ctxName := ""
	if !all {
		b := s.Backend()
		if b == nil {
			return nil, ErrNotConnected
		}
		ctxName = b.Context
	}
	list, err := d.offered(ctxName, all)
	if err != nil {
		return nil, err
	}
	out := make([]DraftInfo, 0, len(list))
	for _, dr := range list {
		out = append(out, dr.info())
	}
	return out, nil
}

// DiscardDraft deletes an offered draft. Its text is gone for good.
func (s *Session) DiscardDraft(id string) error {
	d := s.opts.Drafts
	if d == nil {
		return ErrNoDrafts
	}
	d.wmu.Lock()
	_, err := d.offeredDraft(id)
	if err == nil {
		d.removeFile(id)
	}
	d.wmu.Unlock()
	if err != nil {
		return err
	}
	d.changed("")
	return nil
}

// CreatedText marks text as created in full on a connected context ("" the shown one, D-69): a
// Create resource tab of that context that holds exactly this text has nothing unsaved any more,
// and its draft goes.
func (s *Session) CreatedText(contextID, text string) {
	b := s.BackendOf(contextID)
	if b == nil {
		return
	}
	type changed struct {
		id  string
		rev uint64
	}
	var list []changed
	s.edits.mu.Lock()
	for id, e := range s.edits.m {
		if e.tab.Type == DockCreate && e.tab.Context == b.Context && e.text == text && e.startText != text {
			e.startText = text
			list = append(list, changed{id, e.rev})
		}
	}
	s.edits.mu.Unlock()
	for _, c := range list {
		s.emit(Event{Type: EventEditor, View: c.id, Revision: c.rev})
	}
}
