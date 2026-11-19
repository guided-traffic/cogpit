package engine

import (
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

// The last known user of each context (D-68): the username the API server last confirmed for it
// and when Cogpit last saw it so, kept for the navigator's root row while the cluster is not
// connected. Only the username is kept, never the groups; only a confirmed one, never the
// kubeconfig fallback's label. The file is LastUsersFile beside the cluster settings (Cogpit's
// configuration directory), owner-only and replaced atomically as every file Cogpit keeps there.
// A context's entry goes when its source is removed (forgetUsersOfSource) and at a start at which
// it is certainly gone (forgetUsersOfGoneContexts).

// LastUsersFile is the last known users' file under Cogpit's configuration directory.
const LastUsersFile = "cluster-users.json"

// Bounds of the last known users.
const (
	maxLastUsers     = 1024 // contexts; more are dropped on load, the oldest first on a change
	maxUsernameBytes = 1024 // a longer name is not kept
	lastUsersVersion = 1
)

// lastUser is one context's last known user as kept.
type lastUser struct {
	Username string    `json:"username"`
	Seen     time.Time `json:"seen"`
}

type lastUsersDoc struct {
	Version int                 `json:"version"`
	Users   map[string]lastUser `json:"users"`
}

// userBook is who Cogpit is logged in as, per context: the live answer of the connection to it
// (UserFromAPIServer or UserFromKubeconfig, set at each connect and renewal) and the last known
// user kept on disk. Its mutex is a leaf: nothing is called while it is held, so the UI state's
// sync may read it under the session's lock.
type userBook struct {
	mu     sync.Mutex
	live   map[string]ClusterUser
	last   map[string]lastUser
	path   string // empty: kept in memory only
	dirty  bool
	broken bool // the file did not load: set aside at the first write

	write sync.Mutex // serialises the writes of the file
}

// attach reads the last known users from the cluster settings' directory (in memory only without
// one) before the session's first UI state is shown.
func (b *userBook) attach(s *Session) {
	path := ""
	if c := s.opts.ClusterSettings; c != nil && c.dir != "" {
		path = filepath.Join(c.dir, LastUsersFile)
	}
	var doc lastUsersDoc
	err := readPrefsDoc(path, &doc)
	if err == nil && doc.Version != 0 && doc.Version != lastUsersVersion {
		err = fmt.Errorf("format version %d, this Cogpit reads %d", doc.Version, lastUsersVersion)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.path, b.last, b.live = path, map[string]lastUser{}, map[string]ClusterUser{}
	if err != nil {
		b.broken = true
		s.log.Warn("last known users: the file is unreadable; Cogpit starts without and sets it aside at the next connect", "path", path, "err", err)
		return
	}
	for _, id := range slices.Sorted(maps.Keys(doc.Users)) {
		u := doc.Users[id]
		if len(b.last) == maxLastUsers || checkSettingsContext(id) != nil || !keepableUsername(u.Username) || u.Seen.IsZero() {
			continue
		}
		b.last[id] = lastUser{Username: u.Username, Seen: u.Seen.UTC()}
	}
}

// keepableUsername reports whether a confirmed username may be kept: one line, bounded.
func keepableUsername(name string) bool {
	return name != "" && len(name) <= maxUsernameBytes && utf8.ValidString(name) &&
		strings.IndexFunc(name, unicode.IsControl) < 0
}

// setLive records a connection's answer; a confirmed one is also the context's last known user,
// seen at now.
func (b *userBook) setLive(u ClusterUser, now time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.live == nil {
		b.live, b.last = map[string]ClusterUser{}, map[string]lastUser{}
	}
	b.live[u.Context] = u
	if u.Source == UserFromAPIServer {
		b.seenLocked(u.Context, u.Username, now)
	}
}

// forget drops a context's live answer: a connection without a review (the demo cluster).
func (b *userBook) forget(context string) {
	b.mu.Lock()
	delete(b.live, context)
	b.mu.Unlock()
}

// drop forgets the last known users of the contexts gone reports, live answers stay: a context
// that left Cogpit keeps no trace in the file. It reports whether one went.
func (b *userBook) drop(gone func(string) bool) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(b.last)
	maps.DeleteFunc(b.last, func(id string, _ lastUser) bool { return gone(id) })
	if len(b.last) == n {
		return false
	}
	b.dirty = true
	return true
}

// ids returns the contexts that have a last known user.
func (b *userBook) ids() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return slices.Sorted(maps.Keys(b.last))
}

// seen records that the API server's user of a context was still logged in at now: the
// connection that showed it ended.
func (b *userBook) seen(context, username string, now time.Time) {
	b.mu.Lock()
	b.seenLocked(context, username, now)
	b.mu.Unlock()
}

func (b *userBook) seenLocked(context, username string, now time.Time) {
	if !keepableUsername(username) || checkSettingsContext(context) != nil {
		return
	}
	if b.last == nil {
		b.last = map[string]lastUser{}
	}
	if _, ok := b.last[context]; !ok && len(b.last) >= maxLastUsers {
		oldest := ""
		for id, u := range b.last {
			if oldest == "" || u.Seen.Before(b.last[oldest].Seen) {
				oldest = id
			}
		}
		delete(b.last, oldest)
	}
	b.last[context] = lastUser{Username: username, Seen: now.UTC()}
	b.dirty = true
}

// shown is the user the UI state shows for a context: the connection's answer while it is live,
// else the last known user; nil for none (no context, never confirmed).
func (b *userBook) shown(context string, live bool) *ClusterUser {
	if context == "" {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if u, ok := b.live[context]; ok && live {
		u.Groups = slices.Clone(u.Groups)
		return &u
	}
	if l, ok := b.last[context]; ok {
		return &ClusterUser{Context: context, Username: l.Username, Source: UserLastSeen, LastSeen: l.Seen}
	}
	return nil
}

// flush writes the last known users when they changed since the last write; a failed write is
// logged and tried again at the next change.
func (b *userBook) flush(s *Session) {
	b.mu.Lock()
	dirty := b.dirty
	b.mu.Unlock()
	if !dirty {
		return // every UI change asks: no wait behind a write in progress
	}
	b.write.Lock()
	defer b.write.Unlock()
	b.mu.Lock()
	if !b.dirty || b.path == "" {
		b.dirty = false
		b.mu.Unlock()
		return
	}
	doc := lastUsersDoc{Version: lastUsersVersion, Users: maps.Clone(b.last)}
	path := b.path
	b.dirty = false
	b.mu.Unlock()
	broken := b.isBroken()
	err := writePrefsDoc(path, &broken, doc)
	b.mu.Lock()
	b.broken = broken
	if err != nil {
		b.dirty = true
	}
	b.mu.Unlock()
	if err != nil {
		s.log.Warn("last known users: cannot write the file", "path", path, "err", err)
	}
}

func (b *userBook) isBroken() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.broken
}
