package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"
)

// The hotbar (G13, docs/parity/ui-parity.md › Hotbar): the clusters the user pinned to the bar at
// the window's left edge, top to bottom. Captured from Lens: one tile per cluster with a short
// label, F1–F12 open a cluster. Lens keeps them in banks of twelve with a switcher ("‹ 1 ›") at the
// bottom; Cogpit has one hotbar (the owner's decision of 2026-10-17, D-83): it holds as many tiles
// as the user pins, and the bar scrolls when they do not fit. F1–F12 open the first twelve.
// Inferred (Cogpit's): the tiles are packed in order without empty cells; a context is pinned once;
// a tile whose context no kubeconfig source offers any more stays, marked missing, when its file
// went away by itself; removing its source, or Remove from Cogpit, takes it off (the owner's WF45,
// D-101, dropHotbarTilesOfSource).
//
// The hotbar is engine state (UIState.Hotbar) so that agents see and drive what the user sees; the
// GUI changes it only through the hotbar.* commands. It is Cogpit's own preference: kept in a file
// under Cogpit's configuration directory (HotbarFile, owner-only, replaced atomically), never in a
// kubeconfig, and no hotbar command talks to a cluster except hotbar.openCluster, which connects
// through the same path as cluster.connect.
//
// A file of the banks' time (format version 1) is merged into one hotbar when it is read (D-83): the
// first bank's tiles in their order, then each further bank's in its order, a context pinned in
// several banks once, where it first appears; no tile is dropped. The merged hotbar is written in
// the new format at once, and the old file is kept beside it as hotbar.json.v1.

// Hotbar limits and names.
const (
	// HotbarFile is the hotbar's file under Cogpit's configuration directory.
	HotbarFile = "hotbar.json"
	// HotbarKeys is how many tiles have an F-key: F1 to F12 open the first twelve.
	HotbarKeys = 12
	// HotbarBanksBackup is the name the banks' file (format 1) is kept under after its merge.
	HotbarBanksBackup = HotbarFile + ".v1"
	// maxHotbarContext bounds a stored context id, in bytes: longer ones are dropped on load.
	maxHotbarContext = 4096
	// hotbarVersion is the file's format: 2, one hotbar (D-83); 1 held banks.
	hotbarVersion = 2
	// hotbarBanksVersion is the format of the banks' time, read and merged (D-83).
	hotbarBanksVersion = 1
)

// ErrNoHotbar disables the hotbar commands of a Cogpit without a hotbar (a headless session).
var ErrNoHotbar = errors.New("this Cogpit keeps no hotbar")

// HotbarState is the hotbar: its tiles, top to bottom. Tiles are numbered from 1, as F1 opens the
// first. There is one hotbar, and no limit to its tiles (D-83).
type HotbarState struct {
	Clusters []HotbarTile `json:"clusters"`
}

// HotbarTile is one cluster on the hotbar. Only Context is stored; the other fields describe the
// tile as it is now (Session.HotbarView: ui.state, hotbar.list) and are empty in UIState.Hotbar.
type HotbarTile struct {
	// Context is the context id as cluster.contexts lists it: "<context> [<file>]", or "demo".
	Context string `json:"context"`
	// Label is the context's name without its file.
	Label string `json:"label,omitempty"`
	// DisplayName is the name the tile shows, as the window names the cluster everywhere
	// (ClusterName, D-68): the display name set in its Cluster Settings, else the short name (the
	// part of Label after its last "@"). A name for people: commands take Context.
	DisplayName string `json:"displayName,omitempty"`
	// Connection is the state of the connection to this context (D-69: every connected cluster's,
	// shown or not, as UIState.Connections has it; disconnected without one).
	Connection ConnectionState `json:"connection,omitempty"`
	// Missing is set when no kubeconfig source offers the context any more (its file or folder
	// went away, or the context was renamed): the tile stays until the user removes it. Removing
	// the context's source, or Remove from Cogpit, removes its tile with it (D-101).
	Missing bool `json:"missing,omitempty"`
	// Exec is the approval state of the context's credential plugin (ContextInfo.Exec), so that the
	// GUI shows the plugin for approval before it connects; empty when it needs none.
	Exec string `json:"exec,omitempty"`
	// ExecKind says what needs the approval (ContextInfo.ExecKind).
	ExecKind string `json:"execKind,omitempty"`
}

// Clone returns a deep copy; its tiles are never nil (an empty hotbar is [] in JSON).
func (h HotbarState) Clone() HotbarState {
	out := HotbarState{Clusters: slices.Clone(h.Clusters)}
	if out.Clusters == nil {
		out.Clusters = []HotbarTile{}
	}
	return out
}

// stored is the state as it is kept: tiles with their context only.
func (h HotbarState) stored() HotbarState {
	out := h.Clone()
	for i := range out.Clusters {
		out.Clusters[i] = HotbarTile{Context: out.Clusters[i].Context}
	}
	return out
}

// defaultHotbar is the hotbar of a first start: empty.
func defaultHotbar() HotbarState { return HotbarState{Clusters: []HotbarTile{}} }

// normalize makes a state read from a file one that the commands can work on: tiles of distinct,
// non-empty contexts in their order; a context that appears again is dropped where it repeats.
func (h *HotbarState) normalize() {
	tiles := make([]HotbarTile, 0, len(h.Clusters))
	for _, t := range h.Clusters {
		c := t.Context
		if strings.TrimSpace(c) == "" || len(c) > maxHotbarContext || !utf8.ValidString(c) ||
			slices.ContainsFunc(tiles, func(o HotbarTile) bool { return o.Context == c }) {
			continue
		}
		tiles = append(tiles, HotbarTile{Context: c})
	}
	h.Clusters = tiles
}

// hotbarBank is a bank of a format-1 file: read to be merged (D-83), never written.
type hotbarBank struct {
	Name     string       `json:"name"`
	Clusters []HotbarTile `json:"clusters"`
}

// mergeBanks is the one hotbar a file of banks becomes (D-83): the first bank's tiles in their
// order, then each further bank's, in the banks' order; normalize keeps a context pinned in several
// banks once, where it first appears.
func mergeBanks(banks []hotbarBank) HotbarState {
	var st HotbarState
	for _, b := range banks {
		st.Clusters = append(st.Clusters, b.Clusters...)
	}
	st.normalize()
	return st
}

// Position returns where context is on the hotbar, from 1; 0 when it is not there.
func (h HotbarState) Position(context string) int {
	return slices.IndexFunc(h.Clusters, func(t HotbarTile) bool { return t.Context == context }) + 1
}

// Tile returns the tile in a slot (from 1: F1).
func (h HotbarState) Tile(slot int) (HotbarTile, error) {
	if slot < 1 || slot > len(h.Clusters) {
		return HotbarTile{}, fmt.Errorf("the hotbar has no cluster in slot %d (it has %d)", slot, len(h.Clusters))
	}
	return h.Clusters[slot-1], nil
}

// CheckAdd says why context cannot be added: it is on the hotbar already (wrapping
// ErrNotApplicable, so that menus offer Remove instead).
func (h HotbarState) CheckAdd(context string) error {
	if h.Position(context) > 0 {
		return fmt.Errorf("%w: %s is on the hotbar already", ErrNotApplicable, context)
	}
	return nil
}

// AddCluster adds context at the end of the hotbar.
func (h *HotbarState) AddCluster(context string) error {
	if strings.TrimSpace(context) == "" {
		return errors.New("no context to add")
	}
	if err := h.CheckAdd(context); err != nil {
		return err
	}
	h.Clusters = append(h.Clusters, HotbarTile{Context: context})
	return nil
}

// CheckPinned says why a tile command does not apply: context is not on the hotbar (wrapping
// ErrNotApplicable).
func (h HotbarState) CheckPinned(context string) error {
	if h.Position(context) == 0 {
		return fmt.Errorf("%w: %s is not on the hotbar", ErrNotApplicable, context)
	}
	return nil
}

// RemoveCluster removes context's tile; the tiles below it move up one place.
func (h *HotbarState) RemoveCluster(context string) error {
	if err := h.CheckPinned(context); err != nil {
		return err
	}
	i := h.Position(context) - 1
	h.Clusters = slices.Delete(h.Clusters, i, i+1)
	return nil
}

// CheckMove says why context cannot move to position (from 1).
func (h HotbarState) CheckMove(context string, position int) error {
	if err := h.CheckPinned(context); err != nil {
		return err
	}
	if position < 1 || position > len(h.Clusters) {
		return fmt.Errorf("position %d is outside the hotbar's 1 to %d", position, len(h.Clusters))
	}
	return nil
}

// MoveCluster moves context's tile to position (from 1); the tiles between shift.
func (h *HotbarState) MoveCluster(context string, position int) error {
	if err := h.CheckMove(context, position); err != nil {
		return err
	}
	from := h.Position(context) - 1
	t := h.Clusters[from]
	h.Clusters = slices.Delete(h.Clusters, from, from+1)
	h.Clusters = slices.Insert(h.Clusters, position-1, t)
	return nil
}

// Hotbar keeps the hotbar in HotbarFile under one directory (the app's: <user config dir>/Cogpit).
// It is safe for concurrent use. Two Cogpit processes keep one hotbar each in memory and write the
// file on every change, so the last change wins (the file is not locked, as kubeconfigs.json).
type Hotbar struct {
	dir string // empty: kept in memory only

	// change serialises ChangeHotbar from the write to the UI state it shows, so that the UI states
	// follow the order of the writes; mu guards the state alone, so that a subscriber of the UI
	// state may read the hotbar while a change is shown.
	change sync.Mutex
	mu     sync.Mutex
	state  HotbarState
	loaded bool
	// unreadable is set when the file did not load: it is moved aside to <file>.unreadable before
	// the first save, so that a change never silently overwrites what the user may want back.
	unreadable bool
}

// NewHotbar returns the hotbar kept in dir; an empty dir keeps it in memory only. Nothing is read
// or written until the session attaches it.
func NewHotbar(dir string) *Hotbar { return &Hotbar{dir: dir} }

// Path returns the hotbar's file; empty when it is kept in memory only.
func (h *Hotbar) Path() string {
	if h.dir == "" {
		return ""
	}
	return filepath.Join(h.dir, HotbarFile)
}

// hotbarDoc is the file: format 2 has the tiles; format 1 (and a file without a version, as the
// first builds wrote none) had banks and the shown one, read to be merged (D-83).
type hotbarDoc struct {
	Version  int          `json:"version"`
	Clusters []HotbarTile `json:"clusters"`
	Banks    []hotbarBank `json:"banks,omitempty"`
	Current  int          `json:"current,omitempty"`
}

// load reads the file once: a missing file is the default hotbar; an unreadable one, too, and is
// reported; a file of banks is merged into one hotbar and written in the new format, the old file
// kept beside it (D-83). It returns a note for the log alongside an error. h.mu is held.
func (h *Hotbar) load() (note string, err error) {
	if h.loaded {
		return "", nil
	}
	h.loaded = true
	h.state = defaultHotbar()
	path := h.Path()
	if path == "" {
		return "", nil
	}
	var doc hotbarDoc
	if err := readPrivateJSON(path, maxPrivateDoc, &doc); err != nil {
		h.unreadable = true
		return "", fmt.Errorf("the hotbar file %s is unreadable (%v); Cogpit starts with an empty hotbar and moves the file to %s.unreadable at the first change", path, err, HotbarFile)
	}
	switch {
	case doc.Version == hotbarVersion:
		st := HotbarState{Clusters: doc.Clusters}
		st.normalize()
		h.state = st
		return "", nil
	case doc.Version != 0 && doc.Version != hotbarBanksVersion:
		h.unreadable = true
		return "", fmt.Errorf("the hotbar file %s has format version %d, this Cogpit reads %d; it starts with an empty hotbar and moves the file to %s.unreadable at the first change", path, doc.Version, hotbarVersion, HotbarFile)
	case doc.Version == 0 && doc.Banks == nil: // a missing file
		return "", nil
	}
	h.state = mergeBanks(doc.Banks)
	return h.migrate(path, len(doc.Banks))
}

// migrate keeps a merged file of banks as HotbarBanksBackup and writes the one hotbar in its place
// (D-83). A failure leaves the old file as it is: the merged hotbar shows, and the first change
// writes it. h.mu is held.
func (h *Hotbar) migrate(path string, banks int) (string, error) {
	raw, err := os.ReadFile(filepath.Clean(path)) // Cogpit's own hotbar.json in its config directory
	if err == nil {
		err = writePrivateFile(filepath.Join(h.dir, HotbarBanksBackup), raw)
	}
	if err == nil {
		err = h.save(h.state)
	}
	if err != nil {
		return "", fmt.Errorf("the hotbar's %d banks are merged into one hotbar, but its file is not rewritten yet (%v); the first change writes it", banks, err)
	}
	return fmt.Sprintf("the hotbar's %d banks are merged into one hotbar of %d tiles; the old file is kept as %s", banks, len(h.state.Clusters), HotbarBanksBackup), nil
}

// save writes st to the file, owner-only and atomically (writePrivateFile). h.mu is held.
func (h *Hotbar) save(st HotbarState) error {
	path := h.Path()
	if path == "" {
		return nil
	}
	if h.unreadable {
		if err := renameFile(path, path+".unreadable"); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("hotbar: keeping the unreadable file aside: %w", err)
		}
		h.unreadable = false
	}
	st = st.stored()
	data, err := json.MarshalIndent(hotbarDoc{Version: hotbarVersion, Clusters: st.Clusters}, "", "  ")
	if err != nil {
		return err
	}
	if err := writePrivateFile(path, append(data, '\n')); err != nil {
		return fmt.Errorf("hotbar: %w", err)
	}
	return nil
}

// loadLogged loads the hotbar and logs what the load said. h.mu is held.
func (h *Hotbar) loadLogged(s *Session) {
	note, err := h.load()
	if note != "" {
		s.log.Info("hotbar: " + note)
	}
	if err != nil {
		s.log.Warn("hotbar", "err", err)
	}
}

// attach loads the hotbar into the session's UI state; nil-safe.
func (h *Hotbar) attach(s *Session) {
	if h == nil {
		return
	}
	h.mu.Lock()
	h.loadLogged(s)
	st := h.state.Clone()
	h.mu.Unlock()
	s.ui.Hotbar = &st
}

// Hotbar returns the session's hotbar; nil when it keeps none.
func (s *Session) Hotbar() *Hotbar { return s.opts.Hotbar }

// HotbarState returns the hotbar as stored: tiles with their contexts only.
func (s *Session) HotbarState() (HotbarState, error) {
	h := s.opts.Hotbar
	if h == nil {
		return HotbarState{}, ErrNoHotbar
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.loadLogged(s)
	return h.state.Clone(), nil
}

// ChangeHotbar applies fn to a copy of the hotbar, writes it to the hotbar's file and shows it
// (UIState.Hotbar, EventUIState). If fn or the write fails, nothing changes. Changes are serialised,
// so the UI states follow the order of the writes.
func (s *Session) ChangeHotbar(fn func(*HotbarState) error) (HotbarState, error) {
	h := s.opts.Hotbar
	if h == nil {
		return HotbarState{}, ErrNoHotbar
	}
	h.change.Lock()
	defer h.change.Unlock()
	next, err := func() (HotbarState, error) {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.loadLogged(s)
		next := h.state.Clone()
		if err := fn(&next); err != nil {
			return HotbarState{}, err
		}
		next = next.stored()
		if err := h.save(next); err != nil {
			return HotbarState{}, err
		}
		h.state = next
		return next.Clone(), nil
	}()
	if err != nil {
		return HotbarState{}, err
	}
	shown := next.Clone()
	s.UpdateUI(func(u *UIState) { u.Hotbar = &shown })
	return next, nil
}

// HotbarView returns the hotbar as the user sees it: each tile with its label, the state of a
// connection to it, whether its context is missing, and its credential plugin's approval. A
// context counts as missing only when the contexts could be listed without an error: a kubeconfig
// that fails to load leaves its tiles as they are.
func (s *Session) HotbarView() (HotbarState, error) {
	st, err := s.HotbarState()
	if err != nil {
		return st, err
	}
	known := map[string]ContextInfo{}
	ctxs, cerr := s.Contexts()
	for _, c := range ctxs {
		known[c.Name] = c
	}
	u := s.UIState()
	for i := range st.Clusters {
		t := &st.Clusters[i]
		t.Label = ContextLabel(t.Context)
		t.DisplayName = ClusterName(t.Context, u.ClusterSettings[t.Context])
		t.Connection = Disconnected
		if st := u.connectionState(t.Context); st != "" {
			t.Connection = st
		} else if t.Context == u.Context {
			t.Connection = u.Connection.State
		}
		if c, ok := known[t.Context]; ok {
			t.Exec, t.ExecKind = c.Exec, c.ExecKind
		} else if _, connected := u.ConnectionOf(t.Context); cerr == nil && t.Context != u.Context && !connected {
			t.Missing = true
		}
	}
	return st, nil
}

// ResolveContext returns the id of the context name names: itself when a source or the demo
// offers it, or the one source context with that bare name (D-28), as Connect resolves it.
func (s *Session) ResolveContext(name string) (string, error) {
	if r, ok := s.opts.Connector.(contextResolver); ok {
		return r.ResolveContext(name)
	}
	return name, nil
}

// ContextLabel is the short label of a context id: the context's name without the file it comes
// from ("kind-dev [.kube/config]" → "kind-dev").
func ContextLabel(id string) string {
	if i := strings.LastIndex(id, " ["); i > 0 && strings.HasSuffix(id, "]") {
		return id[:i]
	}
	return id
}
