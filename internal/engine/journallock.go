package engine

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// The drain and Helm journals are files in the user's configuration directory that every Cogpit
// process of the user shares (H-22). Two mechanisms keep the processes apart:
//
//   - The directory lock. Every read and every read-modify-write of a journal file runs under an
//     exclusive lock on <dir>/journals.lock (flock on macOS and Linux, LockFileEx on Windows), so
//     that two processes never lose each other's entries. The lock file is never removed: two
//     processes locking two generations of it would not exclude each other. A process that dies
//     holding it loses it with its file descriptors, so a stale lock cannot exist; a process that
//     hangs while holding it makes the others give up after journalLockWait with an error.
//   - An owner per process. A process that writes a journal entry registers once per directory:
//     it creates <dir>/owners/<id>.lock with a random id and holds an exclusive lock on it for
//     its whole life. Each entry it writes names it (JournalOwner). Whether an entry's owner
//     lives is whether that lock is held: the kernel releases it when the process ends, however
//     it ends, and the id is never reused, so neither PID reuse nor a crash can make a dead
//     process look alive or a live one look dead. Files are opened close-on-exec, so a shell or
//     kubectl that Cogpit starts does not inherit the lock.
//
// An entry whose action is running and whose owner is another live process belongs to that
// process: List marks it (RunsElsewhere), Put refuses to replace it and Remove to remove it.
// Entries of an owner that has ended, and entries without an owner (written before owners
// existed), are interrupted as before; owner files of ended processes are removed when the next
// process registers.

// Names in the journals' directory.
const (
	journalLockFile  = "journals.lock"
	journalOwnersDir = "owners"
	ownerFileSuffix  = ".lock"
)

// journalLockWait bounds how long a journal operation waits for another process's: one holds the
// lock for a single read-modify-write of a small file, so a longer wait means a hung process.
var journalLockWait = 10 * time.Second

// maxOwnerSweep bounds the owner files one registration examines.
const maxOwnerSweep = 256

// errLockBusy is returned by tryLockFile when another open file holds the lock.
var errLockBusy = errors.New("locked by another process")

// ErrJournalBusy is returned when the journals' lock stays held by another Cogpit process for
// longer than a journal operation takes.
var ErrJournalBusy = errors.New("another Cogpit process holds the journal lock")

// ErrRunsElsewhere is matched (errors.Is) by an ElsewhereError.
var ErrRunsElsewhere = errors.New("runs in another Cogpit process")

// JournalOwner names the Cogpit process that recorded a journal entry (H-22).
type JournalOwner struct {
	// ID is random (128 bits, hex) and never reused; the process holds the lock on
	// <dir>/owners/<ID>.lock while it lives.
	ID string `json:"id"`
	// PID is the process's id, for messages and for people reading the file. It never decides
	// whether the owner lives: the system reuses PIDs.
	PID int `json:"pid"`
}

// ElsewhereError refuses to change a journal entry whose action runs in another live Cogpit
// process.
type ElsewhereError struct {
	What string // "the drain of node worker-1"
	PID  int
}

func (e *ElsewhereError) Error() string {
	return fmt.Sprintf("%s runs in another Cogpit process (pid %d); finish or dismiss it there", e.What, e.PID)
}

// Is makes errors.Is(err, ErrRunsElsewhere) hold.
func (e *ElsewhereError) Is(target error) bool { return target == ErrRunsElsewhere }

// journalDir is the journals of one directory as this process sees them: the directory lock and
// the process's owner there. One per directory and process (sharedJournalDir); tests that play two
// processes make a second one with newJournalDir.
type journalDir struct {
	dir string

	// mu serialises this process's journal operations in the directory: the drain and the Helm
	// journal take the directory lock one at a time instead of waiting on each other's lock.
	mu    sync.Mutex
	owner *processOwner // registered on the first write
}

// processOwner is a registered owner: its lock file stays open and locked until the process ends.
type processOwner struct {
	JournalOwner
	f *os.File
}

var journalDirs = struct {
	sync.Mutex
	m map[string]*journalDir
}{m: map[string]*journalDir{}}

// sharedJournalDir returns this process's journalDir for dir.
func sharedJournalDir(dir string) *journalDir {
	key := filepath.Clean(dir)
	if abs, err := filepath.Abs(key); err == nil {
		key = abs
	}
	journalDirs.Lock()
	defer journalDirs.Unlock()
	d := journalDirs.m[key]
	if d == nil {
		d = newJournalDir(dir)
		journalDirs.m[key] = d
	}
	return d
}

func newJournalDir(dir string) *journalDir { return &journalDir{dir: dir} }

// locked runs fn under the directory lock. With create false and no directory, fn runs without
// the lock: there is nothing to read, and a read must not create the configuration directory.
func (d *journalDir) locked(create bool, fn func() error) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !create {
		if _, err := os.Stat(d.dir); errors.Is(err, fs.ErrNotExist) {
			return fn()
		}
	}
	if err := os.MkdirAll(d.dir, 0o700); err != nil {
		return err
	}
	f, err := lockPath(filepath.Join(d.dir, journalLockFile), journalLockWait)
	if err != nil {
		return err
	}
	defer func() {
		_ = unlockFile(f)
		_ = f.Close()
	}()
	return fn()
}

// self returns the process's owner, registering it first; it runs under the directory lock.
func (d *journalDir) self() (*JournalOwner, error) {
	if d.owner != nil {
		return &d.owner.JournalOwner, nil
	}
	owners := filepath.Join(d.dir, journalOwnersDir)
	if err := os.MkdirAll(owners, 0o700); err != nil {
		return nil, err
	}
	d.sweep(owners)
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return nil, err
	}
	id := hex.EncodeToString(raw[:])
	f, err := createOwnerFile(filepath.Join(owners, id+ownerFileSuffix))
	if err != nil {
		return nil, err
	}
	// Created under the directory lock, which every check of an owner holds too: nobody sees the
	// file before it is locked.
	if err := tryLockFile(f); err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return nil, err
	}
	d.owner = &processOwner{JournalOwner: JournalOwner{ID: id, PID: os.Getpid()}, f: f}
	return &d.owner.JournalOwner, nil
}

// selfID returns the process's owner id in the directory, or "" before it wrote an entry.
func (d *journalDir) selfID() string {
	if d.owner == nil {
		return ""
	}
	return d.owner.ID
}

// sweep removes the owner files of processes that ended, at most maxOwnerSweep of them; it runs
// under the directory lock. A dead owner's id is never used again, so removing its file cannot
// race with that owner.
func (d *journalDir) sweep(owners string) {
	entries, err := os.ReadDir(owners)
	if err != nil {
		return
	}
	for i, e := range entries {
		if i >= maxOwnerSweep {
			return
		}
		id, ok := strings.CutSuffix(e.Name(), ownerFileSuffix)
		if !ok || !validOwnerID(id) || id == d.selfID() {
			continue
		}
		if !ownerFileAlive(filepath.Join(owners, e.Name())) {
			_ = os.Remove(filepath.Join(owners, e.Name()))
		}
	}
}

// alive reports whether an entry's owner is a live process other than this one; it runs under
// the directory lock. An entry without an owner was written before owners existed.
func (d *journalDir) aliveElsewhere(o *JournalOwner) bool {
	if o == nil || !validOwnerID(o.ID) || o.ID == d.selfID() {
		return false
	}
	return ownerFileAlive(filepath.Join(d.dir, journalOwnersDir, o.ID+ownerFileSuffix))
}

// ownerFileAlive reports whether a process holds the lock on an owner file. A missing file is a
// process that ended (a sweep removed it). A lock that cannot be tried for another reason counts
// as held: Cogpit then does not offer to finish an action that may be running.
func ownerFileAlive(path string) bool {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return !errors.Is(err, fs.ErrNotExist)
	}
	defer f.Close()
	switch err := tryLockFile(f); {
	case err == nil:
		_ = unlockFile(f)
		return false
	case errors.Is(err, errLockBusy):
		return true
	default:
		return true
	}
}

// validOwnerID accepts the ids self makes: 32 lowercase hex digits. An id from the file is
// untrusted and becomes part of a path.
func validOwnerID(id string) bool {
	if len(id) != 32 {
		return false
	}
	for _, c := range id {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// lockPath opens path (creating it owner-only) and locks it exclusively, waiting at most wait.
// The caller unlocks and closes the file. When the wait runs out, the waiting goroutine keeps the
// file: it unlocks and closes it once the lock arrives, so the caller must not touch it.
func lockPath(path string, wait time.Duration) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	switch err := tryLockFile(f); {
	case err == nil:
		return f, nil
	case !errors.Is(err, errLockBusy):
		_ = f.Close()
		return nil, err
	}
	// Another process holds it: wait in the kernel, which wakes the waiters when it is released,
	// rather than polling, which a busy holder could starve.
	done := make(chan error, 1)
	go func() { done <- lockFile(f) }()
	t := time.NewTimer(wait)
	defer t.Stop()
	select {
	case err := <-done:
		if err != nil {
			_ = f.Close()
			return nil, err
		}
		return f, nil
	case <-t.C:
		go func() {
			if err := <-done; err == nil {
				_ = unlockFile(f)
			}
			_ = f.Close()
		}()
		return nil, fmt.Errorf("%w (%s, waited %s)", ErrJournalBusy, path, wait)
	}
}
