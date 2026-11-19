package nativedialog

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"time"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// The bookmarks of the items picked in the system's open panel that Cogpit reads again in later
// runs (D-72): the kubeconfig files and sync folders added as sources, a terminal's working
// directory and the kubectl folder, a chat connection's CA file. One owner-only JSON file in
// Cogpit's configuration directory, written whole through engine.WritePrivateFile (0600 in a 0700
// directory, replaced atomically); no keychain (the owner's rule). At start the app resolves them
// and starts accessing their items before anything reads a source (cmd/cogpit), so that macOS
// can see the access the user once granted in the panel. A bookmark is made again by the next
// pick, so an unreadable file is not kept aside: the next pick replaces it.

// BookmarksFile is the store's file in Cogpit's configuration directory.
const BookmarksFile = "bookmarks.json"

// Uses of a bookmark: what refers to its item, which decides when the bookmark goes.
const (
	// UseKubeconfig: a kubeconfig source, a file or a sync folder; kept while it is a source.
	UseKubeconfig = "kubeconfig"
	// UseFolder: a cluster's terminal working directory or the folder for kubectl binaries.
	UseFolder = "folder"
	// UseCAFile: a chat connection's CA file.
	UseCAFile = "caFile"
)

// Bounds of the store: the newest bookmarks of a use are kept. A kubeconfig source's bookmark
// goes with the source; the others, whose settings Cogpit does not look into at start, go when
// sixteen newer ones of their use came.
const (
	maxKubeconfigBookmarks = 256
	maxOtherBookmarks      = 16
	maxStoreSize           = 4 << 20
	storeVersion           = 1
)

// Entry is one kept bookmark.
type Entry struct {
	// Use is what refers to the item (UseKubeconfig, UseFolder, UseCAFile).
	Use string `json:"use"`
	// Path is the item's path as the panel returned it, cleaned.
	Path string `json:"path"`
	// Access is the bookmark's access.
	Access Access `json:"access"`
	// Bookmark is the security-scoped bookmark.
	Bookmark []byte `json:"bookmark"`
	// Added is when it was made or last refreshed.
	Added time.Time `json:"added"`
}

type storeDoc struct {
	Version   int     `json:"version"`
	Bookmarks []Entry `json:"bookmarks"`
}

// resolved is what resolving a bookmark gave: the item's path now, whether access started, and a
// fresh bookmark when the old one was stale.
type resolved struct {
	path      string
	accessing bool
	fresh     []byte
}

// Store keeps the bookmarks. A nil *Store keeps nothing. Safe for concurrent use.
type Store struct {
	path    string
	mu      sync.Mutex
	entries []Entry
	resolve func([]byte, Access) (resolved, error)
	now     func() time.Time
}

// Report is what Restore did, for the log: no paths, only counts.
type Report struct {
	// Restored bookmarks resolved; Accessing of them started accessing their item.
	Restored, Accessing int
	// Refreshed were stale and replaced by a fresh bookmark.
	Refreshed int
	// Dropped were no longer referred to.
	Dropped int
	// Failed did not resolve (an item gone, a volume not mounted); they are kept.
	Failed int
}

// OpenStore reads the store at path; an empty path keeps the bookmarks for the run only. A file
// that does not load gives an empty store and the error, and the next Keep replaces the file.
func OpenStore(path string) (*Store, error) {
	s := &Store{path: path, resolve: resolveBookmark, now: time.Now}
	if path == "" {
		return s, nil
	}
	var doc storeDoc
	if err := engine.ReadPrivateJSON(path, maxStoreSize, &doc); err != nil {
		return s, err
	}
	s.entries = doc.Bookmarks
	return s, nil
}

// Keep stores the bookmark of path for use, replacing one of the same use and path. Nothing is
// kept without a bookmark.
func (s *Store) Keep(use, path string, access Access, bookmark []byte) error {
	if s == nil || len(bookmark) == 0 || path == "" {
		return nil
	}
	path = filepath.Clean(path)
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Entry, 0, len(s.entries)+1)
	for _, e := range s.entries {
		if e.Use != use || e.Path != path {
			out = append(out, e)
		}
	}
	s.entries = capEntries(append(out, Entry{Use: use, Path: path, Access: access, Bookmark: bookmark, Added: s.now().UTC()}))
	return s.save()
}

// Restore resolves every bookmark keep accepts and starts accessing its item for the rest of the
// process's life, replaces a stale bookmark by a fresh one, drops those keep refuses, and writes
// the store when that changed it. Call it at start, before anything reads the items. In a build
// without bookmarks it only drops what keep refuses.
func (s *Store) Restore(keep func(Entry) bool) (Report, error) {
	var rep Report
	if s == nil {
		return rep, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	changed := false
	out := make([]Entry, 0, len(s.entries))
	for _, e := range s.entries {
		if keep != nil && !keep(e) {
			rep.Dropped++
			changed = true
			continue
		}
		r, err := s.resolve(e.Bookmark, e.Access)
		switch {
		case errors.Is(err, ErrUnsupported):
		case err != nil:
			rep.Failed++
		default:
			rep.Restored++
			if r.accessing {
				rep.Accessing++
			}
			if len(r.fresh) > 0 {
				e.Bookmark, e.Added = r.fresh, s.now().UTC()
				rep.Refreshed++
				changed = true
			}
		}
		out = append(out, e)
	}
	capped := capEntries(out)
	if len(capped) != len(out) {
		rep.Dropped += len(out) - len(capped)
		changed = true
	}
	s.entries = capped
	if !changed {
		return rep, nil
	}
	return rep, s.save()
}

// Drop drops the bookmarks keep refuses and writes the store when that changed it, without
// resolving any: a removed kubeconfig source's bookmark goes with the source. It returns how many
// went; a nil keep drops nothing.
func (s *Store) Drop(keep func(Entry) bool) (int, error) {
	if s == nil || keep == nil {
		return 0, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Entry, 0, len(s.entries))
	for _, e := range s.entries {
		if keep(e) {
			out = append(out, e)
		}
	}
	dropped := len(s.entries) - len(out)
	if dropped == 0 {
		return 0, nil
	}
	s.entries = out
	return dropped, s.save()
}

// Entries returns a copy of the kept bookmarks.
func (s *Store) Entries() []Entry {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Entry(nil), s.entries...)
}

// save writes the store; s.mu is held.
func (s *Store) save() error {
	if s.path == "" {
		return nil
	}
	data, err := json.MarshalIndent(storeDoc{Version: storeVersion, Bookmarks: s.entries}, "", "  ")
	if err != nil {
		return err
	}
	return engine.WritePrivateFile(s.path, append(data, '\n'))
}

// capEntries keeps the newest entries of each use within its bound, in their order.
func capEntries(entries []Entry) []Entry {
	seen := map[string]int{}
	keep := make([]bool, len(entries))
	n := 0
	for i := len(entries) - 1; i >= 0; i-- {
		use := entries[i].Use
		limit := maxOtherBookmarks
		if use == UseKubeconfig {
			limit = maxKubeconfigBookmarks
		}
		if seen[use] < limit {
			seen[use]++
			keep[i] = true
			n++
		}
	}
	if n == len(entries) {
		return entries
	}
	out := make([]Entry, 0, n)
	for i, e := range entries {
		if keep[i] {
			out = append(out, e)
		}
	}
	return out
}
