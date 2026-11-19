package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/guided-traffic/cogpit/internal/model"
)

// The node shell journal (H-71, owner; D-11): every node shell pod Cogpit creates is recorded
// before its create is sent, with its context, namespace, name and a random id the pod carries as
// an annotation (NodeShellIDAnnotation), and gets its UID once the API server returned it. The
// entry goes when the pod is deleted (the shell ends, its tab closes, Cogpit quits) or is gone. A
// crash, a kill or a lost connection during the delete leave the entry, and the next connect to
// that context deletes the pod (nodeshellsweep.go). Recording before the create means a crash
// between the create and the journal cannot leave a pod nobody recorded: the worst case is an entry
// without a UID, whose pod the sweep finds by name and recognises by the id.

// NodeShellEntry is one node shell pod Cogpit created and has not seen deleted.
type NodeShellEntry struct {
	Context   string `json:"context"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	// UID is the pod's UID as the create returned it; empty when Cogpit ended between the create
	// and recording it.
	UID string `json:"uid,omitempty"`
	// ID is the random value of the pod's NodeShellIDAnnotation, which tells Cogpit's pod from
	// another of the same name when the UID is not known.
	ID      string    `json:"id"`
	Node    string    `json:"node"`
	Created time.Time `json:"created"`
	// Owner is the Cogpit process that recorded the entry (H-22, journallock.go).
	Owner *JournalOwner `json:"owner,omitempty"`

	// elsewhere: List found the entry's process alive, another than this one.
	elsewhere bool
}

// OwnedElsewhere reports whether another live Cogpit process of the user recorded the entry: its
// node shell may be open there, so it is not this process's to delete.
func (e NodeShellEntry) OwnedElsewhere() bool { return e.elsewhere }

func (e NodeShellEntry) key() nodeShellKey { return nodeShellKey{e.Context, e.Namespace, e.Name} }

type nodeShellKey struct{ context, namespace, name string }

// NodeShellJournal keeps the node shell pods Cogpit created across restarts. Implementations are
// safe for concurrent use.
type NodeShellJournal interface {
	// List returns every entry; a journal that cannot be read returns an error, never a partial
	// list.
	List() ([]NodeShellEntry, error)
	// Put adds an entry or replaces the one for the same context, namespace and name.
	Put(NodeShellEntry) error
	// Remove deletes the entry for a context, namespace and name; a missing one is no error.
	Remove(context, namespace, name string) error
}

// MemoryNodeShellJournal is a NodeShellJournal that lives as long as the process: for tests and
// for a session without a configuration directory, where a pod a crash leaves lives until its
// deadline.
type MemoryNodeShellJournal struct {
	mu      sync.Mutex
	entries []NodeShellEntry
}

// NewMemoryNodeShellJournal returns an empty in-memory journal.
func NewMemoryNodeShellJournal() *MemoryNodeShellJournal { return &MemoryNodeShellJournal{} }

// List implements NodeShellJournal.
func (m *MemoryNodeShellJournal) List() ([]NodeShellEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.entries), nil
}

// Put implements NodeShellJournal.
func (m *MemoryNodeShellJournal) Put(e NodeShellEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries = append(removeShellEntry(m.entries, e.key()), e)
	return nil
}

// Remove implements NodeShellJournal.
func (m *MemoryNodeShellJournal) Remove(context, namespace, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries = removeShellEntry(m.entries, nodeShellKey{context, namespace, name})
	return nil
}

func removeShellEntry(entries []NodeShellEntry, k nodeShellKey) []NodeShellEntry {
	return slices.DeleteFunc(slices.Clone(entries), func(e NodeShellEntry) bool { return e.key() == k })
}

// NodeShellJournalFile is the journal's file name in Cogpit's configuration directory.
const NodeShellJournalFile = "nodeshells.json"

// nodeShellJournalVersion is the file format's version; a file with another one is not read.
const nodeShellJournalVersion = 1

// nodeShellJournalDoc is the file: {"version":1,"nodeShells":[{"context":…,"name":…},…]}.
type nodeShellJournalDoc struct {
	Version    int              `json:"version"`
	NodeShells []NodeShellEntry `json:"nodeShells"`
}

// FileNodeShellJournal is the NodeShellJournal of the desktop app, as the drain journal is: one
// JSON file, owner-only (0600) in an owner-only directory (0700), replaced atomically on every
// change, read and written under the journals' directory lock that every Cogpit process of the user
// shares, each entry naming the process that wrote it (journallock.go). A file that cannot be
// parsed is reported, not overwritten.
type FileNodeShellJournal struct {
	path string
	dir  *journalDir
}

// NewFileNodeShellJournal returns the journal at <dir>/nodeshells.json. Nothing is read or written
// until it is used.
func NewFileNodeShellJournal(dir string) *FileNodeShellJournal {
	return newFileNodeShellJournal(sharedJournalDir(dir))
}

func newFileNodeShellJournal(d *journalDir) *FileNodeShellJournal {
	return &FileNodeShellJournal{path: filepath.Join(d.dir, NodeShellJournalFile), dir: d}
}

// Path returns the journal file's path.
func (j *FileNodeShellJournal) Path() string { return j.path }

// NodeShellJournalError reports a journal file that cannot be read.
type NodeShellJournalError struct {
	Path string
	Err  error
}

func (e *NodeShellJournalError) Error() string {
	return fmt.Sprintf("the node shell journal %s is unreadable (%v); Cogpit leaves it as it is: move it aside to start a new one", e.Path, e.Err)
}

// Unwrap returns the cause.
func (e *NodeShellJournalError) Unwrap() error { return e.Err }

// List implements NodeShellJournal.
func (j *FileNodeShellJournal) List() (entries []NodeShellEntry, err error) {
	err = j.dir.locked(false, func() error {
		entries, err = j.load()
		for i, e := range entries {
			entries[i].elsewhere = j.dir.aliveElsewhere(e.Owner)
		}
		return err
	})
	return entries, err
}

// Put implements NodeShellJournal.
func (j *FileNodeShellJournal) Put(e NodeShellEntry) error {
	return j.dir.locked(true, func() error {
		entries, err := j.load()
		if err != nil {
			return err
		}
		self, err := j.dir.self()
		if err != nil {
			return fmt.Errorf("node shell journal: %w", err)
		}
		e.Owner, e.elsewhere = self, false
		return j.save(append(removeShellEntry(entries, e.key()), e))
	})
}

// Remove implements NodeShellJournal.
func (j *FileNodeShellJournal) Remove(context, namespace, name string) error {
	return j.dir.locked(false, func() error {
		entries, err := j.load()
		if err != nil {
			return err
		}
		k := nodeShellKey{context, namespace, name}
		if !slices.ContainsFunc(entries, func(e NodeShellEntry) bool { return e.key() == k }) {
			return nil
		}
		return j.save(removeShellEntry(entries, k))
	})
}

func (j *FileNodeShellJournal) load() ([]NodeShellEntry, error) {
	info, err := os.Stat(j.path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, &NodeShellJournalError{Path: j.path, Err: err}
	}
	if info.Size() > maxJournalSize {
		return nil, &NodeShellJournalError{Path: j.path, Err: fmt.Errorf("%s is not a node shell journal", model.FormatBytes(info.Size()))}
	}
	data, err := os.ReadFile(j.path)
	if err != nil {
		return nil, &NodeShellJournalError{Path: j.path, Err: err}
	}
	var doc nodeShellJournalDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, &NodeShellJournalError{Path: j.path, Err: err}
	}
	if doc.Version != nodeShellJournalVersion {
		return nil, &NodeShellJournalError{Path: j.path,
			Err: fmt.Errorf("format version %d, this Cogpit reads %d", doc.Version, nodeShellJournalVersion)}
	}
	for _, e := range doc.NodeShells {
		if e.Context == "" || e.Namespace == "" || e.Name == "" || e.ID == "" {
			return nil, &NodeShellJournalError{Path: j.path, Err: errors.New("an entry without a context, namespace, name or id")}
		}
	}
	return doc.NodeShells, nil
}

func (j *FileNodeShellJournal) save(entries []NodeShellEntry) error {
	if entries == nil {
		entries = []NodeShellEntry{}
	}
	data, err := json.MarshalIndent(nodeShellJournalDoc{Version: nodeShellJournalVersion, NodeShells: entries}, "", "  ")
	if err != nil {
		return fmt.Errorf("node shell journal: %w", err)
	}
	if err := writeFileAtomic(j.path, append(data, '\n')); err != nil {
		return fmt.Errorf("node shell journal: %w", err)
	}
	return nil
}
