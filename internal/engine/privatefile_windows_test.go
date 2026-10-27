//go:build windows

package engine

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// holdOpen opens path as another process's reader does (os.Open shares reading and writing, not
// deletion): until the returned func closes it, Windows refuses a rename over the file and its
// removal.
func holdOpen(t *testing.T, path string) func() {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	return func() { _ = f.Close() }
}

// A file another process holds open for a moment (another Cogpit reading it, an editor, a virus
// scanner) is replaced, or removed, once the hold ends: retryHeld runs the rename or the removal
// again within heldRetry.
func TestHeldFileReplacedAndRemovedOnceLetGo(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := writePrivateFile(path, []byte("old")); err != nil {
		t.Fatal(err)
	}
	time.AfterFunc(100*time.Millisecond, holdOpen(t, path))
	if err := writePrivateFile(path, []byte("new")); err != nil {
		t.Fatalf("a write while the file was held for a moment: %v", err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "new" {
		t.Errorf("after the hold: %q, %v", data, err)
	}

	time.AfterFunc(100*time.Millisecond, holdOpen(t, path))
	if err := retryHeld(func() error { return os.Remove(path) }); err != nil {
		t.Fatalf("a removal while the file was held for a moment: %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the file is still there: %v", err)
	}
}

// A hold that outlasts heldRetry gives the error back, naming the file, and leaves the old content
// and no temporary file: the change is reported, not lost.
func TestHeldFileRefusedPastHeldRetry(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	if err := writePrivateFile(path, []byte("old")); err != nil {
		t.Fatal(err)
	}
	release := holdOpen(t, path)
	err := writePrivateFile(path, []byte("new"))
	release()
	if err == nil || !heldOpenElsewhere(err) || !strings.Contains(err.Error(), path) {
		t.Fatalf("a write while the file stays held: %v", err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "old" {
		t.Errorf("after the refused write: %q, %v", data, err)
	}
	if entries, err := os.ReadDir(dir); err != nil || len(entries) != 1 {
		t.Errorf("the folder holds %d files, want settings.json alone (%v)", len(entries), err)
	}
}
