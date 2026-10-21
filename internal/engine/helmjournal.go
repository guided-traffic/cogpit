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

// HelmAction names a release action the journal records.
type HelmAction string

// Release actions.
const (
	HelmRollback  HelmAction = "rollback"
	HelmUninstall HelmAction = "uninstall"
	// HelmUpgrade: an upgrade of a release to a chart version with new values (helm upgrade). The
	// entry names the chart and version, never the values: they can hold credentials, and the
	// release's pending revision in the cluster holds them anyway. An interrupted upgrade leaves the
	// release pending-upgrade, which the Helm SDK's upgrade refuses; its remedy is a rollback to
	// FromRevision (architecture §8).
	HelmUpgrade HelmAction = "upgrade"
	// HelmInstall: an install of a chart as a new release (helm install, helminstall.go). The entry
	// names the chart ("<repository>/<chart>") and version, never the values. An interrupted
	// install leaves the release pending-install (or failed); Cogpit offers nothing to finish it
	// (owner, 2026-10-14 evening, A17): the entry shows the release's state until the cluster shows
	// the install finished or the release gone, the user dismisses it, or another action on the
	// release (an uninstall in the Helm Releases view) replaces it. The journal still records the
	// install: it keeps a second Cogpit process from starting an action on the release while the
	// install runs (H-22), and tells the user after a crash what was left. An entry of this kind
	// makes the journal unreadable for a Cogpit from before 2026-10-13.
	HelmInstall HelmAction = "install"
)

// known reports whether the journal knows how to offer an action.
func (a HelmAction) known() bool {
	return a == HelmRollback || a == HelmUninstall || a == HelmUpgrade || a == HelmInstall
}

// HelmOpState is a journal entry's state.
type HelmOpState string

// Release action states.
const (
	// HelmOpRunning: the action was started. An entry that a later start of Cogpit finds in this
	// state was interrupted: Cogpit ended before the Helm SDK returned, and the release may be left
	// pending-rollback or uninstalling.
	HelmOpRunning HelmOpState = "running"
	// HelmOpFailed: the Helm SDK returned an error; the release may be failed or uninstalling.
	HelmOpFailed HelmOpState = "failed"
)

// PendingHelmOp is a release action Cogpit started and has not seen finish: recorded before the
// Helm SDK runs, removed when it returns without an error (D-11).
type PendingHelmOp struct {
	Context   string     `json:"context"`
	Namespace string     `json:"namespace"`
	Release   string     `json:"release"`
	Action    HelmAction `json:"action"`
	// Revision is the revision a rollback goes back to.
	Revision int `json:"revision,omitempty"`
	// FromRevision is the release's newest revision when the action started.
	FromRevision int `json:"fromRevision,omitempty"`
	// Chart and ChartVersion are what an upgrade installs (the chart's name and version) or an
	// install installs ("<repository>/<chart>" and the version).
	Chart        string      `json:"chart,omitempty"`
	ChartVersion string      `json:"chartVersion,omitempty"`
	Started      time.Time   `json:"started"`
	StartedBy    Origin      `json:"startedBy"`
	State        HelmOpState `json:"state"`
	Error        string      `json:"error,omitempty"`
	// Owner is the Cogpit process that recorded the entry (H-22, journallock.go). Entries written
	// before owners existed have none and count as recorded by a process that has ended.
	Owner *JournalOwner `json:"owner,omitempty"`

	// elsewhere: List found the action running in another live Cogpit process.
	elsewhere bool
}

func (p PendingHelmOp) key() helmKey { return helmKey{p.Context, p.Namespace, p.Release} }

// RunsElsewhere reports whether the journal found the action running in another live Cogpit
// process of the user: not interrupted, and not this process's to finish or dismiss.
func (p PendingHelmOp) RunsElsewhere() bool { return p.elsewhere }

func (p PendingHelmOp) what() string {
	return fmt.Sprintf("the %s of release %s/%s", p.Action, p.Namespace, p.Release)
}

// helmKey names a release in a context: at most one action per release is recorded.
type helmKey struct{ context, namespace, release string }

// HelmJournal keeps the pending release actions across restarts of Cogpit. Implementations are
// safe for concurrent use.
type HelmJournal interface {
	// List returns every entry. A journal that cannot be read returns an error, never a partial
	// list.
	List() ([]PendingHelmOp, error)
	// Put adds an entry or replaces the one for the same context, namespace and release. A journal
	// shared between processes refuses, with an ElsewhereError, to replace an entry whose action
	// runs in another live process.
	Put(PendingHelmOp) error
	// Remove deletes the entry for a release; a missing entry is not an error. A shared journal
	// refuses, with an ElsewhereError, an entry whose action runs in another live process.
	Remove(context, namespace, release string) error
}

// MemoryHelmJournal is a HelmJournal that lives as long as the process: for tests and for a
// session without a configuration directory.
type MemoryHelmJournal struct {
	mu      sync.Mutex
	entries []PendingHelmOp
}

// NewMemoryHelmJournal returns an empty in-memory journal.
func NewMemoryHelmJournal() *MemoryHelmJournal { return &MemoryHelmJournal{} }

// List implements HelmJournal.
func (m *MemoryHelmJournal) List() ([]PendingHelmOp, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.entries), nil
}

// Put implements HelmJournal.
func (m *MemoryHelmJournal) Put(p PendingHelmOp) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries = putHelmEntry(m.entries, p)
	return nil
}

// Remove implements HelmJournal.
func (m *MemoryHelmJournal) Remove(context, namespace, release string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries = removeHelmEntry(m.entries, helmKey{context, namespace, release})
	return nil
}

func putHelmEntry(entries []PendingHelmOp, p PendingHelmOp) []PendingHelmOp {
	return append(removeHelmEntry(entries, p.key()), p)
}

func removeHelmEntry(entries []PendingHelmOp, k helmKey) []PendingHelmOp {
	return slices.DeleteFunc(slices.Clone(entries), func(e PendingHelmOp) bool { return e.key() == k })
}

// HelmJournalFile is the journal's file name in Cogpit's configuration directory.
const HelmJournalFile = "helm.json"

// helmJournalVersion is the file format's version; a file with another one is not read.
const helmJournalVersion = 1

// helmJournalDoc is the file: {"version":1,"operations":[{"context":…,"release":…},…]}.
type helmJournalDoc struct {
	Version    int             `json:"version"`
	Operations []PendingHelmOp `json:"operations"`
}

// FileHelmJournal is the HelmJournal of the desktop app, made like the drain journal: one JSON
// file, owner-only (0600) in an owner-only directory (0700), replaced atomically on every change,
// so that a crash leaves the old or the new journal, never a torn one. A file that cannot be
// parsed is reported, not overwritten: it may be the only record of a release left pending.
//
// Every Cogpit process of the user shares the file, under the same directory lock and owners as
// the drain journal (H-22, journallock.go).
type FileHelmJournal struct {
	path string
	dir  *journalDir
}

// NewFileHelmJournal returns the journal at <dir>/helm.json. Nothing is read or written until it
// is used.
func NewFileHelmJournal(dir string) *FileHelmJournal {
	return newFileHelmJournal(sharedJournalDir(dir))
}

func newFileHelmJournal(d *journalDir) *FileHelmJournal {
	return &FileHelmJournal{path: filepath.Join(d.dir, HelmJournalFile), dir: d}
}

// Path returns the journal file's path.
func (j *FileHelmJournal) Path() string { return j.path }

// HelmJournalError reports a journal file that cannot be read.
type HelmJournalError struct {
	Path string
	Err  error
}

func (e *HelmJournalError) Error() string {
	return fmt.Sprintf("the Helm journal %s is unreadable (%v); Cogpit leaves it as it is: move it aside to start a new one", e.Path, e.Err)
}

// Unwrap returns the cause.
func (e *HelmJournalError) Unwrap() error { return e.Err }

// List implements HelmJournal.
func (j *FileHelmJournal) List() (entries []PendingHelmOp, err error) {
	err = j.dir.locked(false, func() error {
		entries, err = j.load()
		for i, e := range entries {
			entries[i].elsewhere = e.State == HelmOpRunning && j.dir.aliveElsewhere(e.Owner)
		}
		return err
	})
	return entries, err
}

// Put implements HelmJournal.
func (j *FileHelmJournal) Put(p PendingHelmOp) error {
	return j.dir.locked(true, func() error {
		entries, err := j.load()
		if err != nil {
			return err
		}
		if err := j.checkOwner(entries, p.key()); err != nil {
			return err
		}
		self, err := j.dir.self()
		if err != nil {
			return fmt.Errorf("helm journal: %w", err)
		}
		p.Owner, p.elsewhere = self, false
		return j.save(putHelmEntry(entries, p))
	})
}

// Remove implements HelmJournal.
func (j *FileHelmJournal) Remove(context, namespace, release string) error {
	return j.dir.locked(false, func() error {
		entries, err := j.load()
		if err != nil {
			return err
		}
		k := helmKey{context, namespace, release}
		if !slices.ContainsFunc(entries, func(e PendingHelmOp) bool { return e.key() == k }) {
			return nil
		}
		if err := j.checkOwner(entries, k); err != nil {
			return err
		}
		return j.save(removeHelmEntry(entries, k))
	})
}

// checkOwner refuses to change the entry for k while its action runs in another live process.
func (j *FileHelmJournal) checkOwner(entries []PendingHelmOp, k helmKey) error {
	for _, e := range entries {
		if e.key() == k && e.State == HelmOpRunning && j.dir.aliveElsewhere(e.Owner) {
			return &ElsewhereError{What: e.what(), PID: e.Owner.PID}
		}
	}
	return nil
}

func (j *FileHelmJournal) load() ([]PendingHelmOp, error) {
	info, err := os.Stat(j.path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, &HelmJournalError{Path: j.path, Err: err}
	}
	if info.Size() > maxJournalSize {
		return nil, &HelmJournalError{Path: j.path, Err: fmt.Errorf("%s is not a Helm journal", model.FormatBytes(info.Size()))}
	}
	data, err := os.ReadFile(j.path)
	if err != nil {
		return nil, &HelmJournalError{Path: j.path, Err: err}
	}
	var doc helmJournalDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, &HelmJournalError{Path: j.path, Err: err}
	}
	if doc.Version != helmJournalVersion {
		return nil, &HelmJournalError{Path: j.path, Err: fmt.Errorf("format version %d, this Cogpit reads %d", doc.Version, helmJournalVersion)}
	}
	for _, e := range doc.Operations {
		if e.Context == "" || e.Namespace == "" || e.Release == "" || !e.Action.known() {
			return nil, &HelmJournalError{Path: j.path, Err: errors.New("an entry without a context, namespace, release or known action")}
		}
	}
	return doc.Operations, nil
}

func (j *FileHelmJournal) save(entries []PendingHelmOp) error {
	if entries == nil {
		entries = []PendingHelmOp{}
	}
	data, err := json.MarshalIndent(helmJournalDoc{Version: helmJournalVersion, Operations: entries}, "", "  ")
	if err != nil {
		return fmt.Errorf("helm journal: %w", err)
	}
	if err := writeFileAtomic(j.path, append(data, '\n')); err != nil {
		return fmt.Errorf("helm journal: %w", err)
	}
	return nil
}

// writeFileAtomic replaces path with data, owner-only, in an owner-only directory: a temporary
// file in the same directory, synced, renamed over the old one, so that a crash leaves the old or
// the new content. Both journals save through it.
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+"-*.tmp") // created 0600
	if err != nil {
		return err
	}
	written := false
	defer func() {
		if !written {
			_ = os.Remove(tmp.Name())
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if journalSync {
		if err := tmp.Sync(); err != nil {
			_ = tmp.Close()
			return err
		}
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := renameFile(tmp.Name(), path); err != nil {
		return err
	}
	written = true
	if journalSync {
		syncDir(dir)
	}
	return nil
}

// journalSync makes journal writes durable before they count (fsync; F_FULLFSYNC on macOS, about
// 13 ms of a 13.4 ms write there). Only stress tests of the journals' lock turn it off: the lock,
// not the sync, keeps processes from losing each other's entries.
var journalSync = true
