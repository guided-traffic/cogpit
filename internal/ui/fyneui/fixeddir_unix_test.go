//go:build unix

package fyneui

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
)

// fixedHeld are the fixed folders this process holds (fixedDir), so that a second claim within a
// test falls back to a temporary folder instead of waiting for its own lock.
var (
	fixedMu   sync.Mutex
	fixedHeld = map[string]bool{}
)

// fixedDir is an empty folder whose path is the same in every run, /tmp/cogpit-tests/<test name>,
// for a test whose picture shows a path (a kubeconfig's file, the sync picker's folder): a
// temporary folder's random name would change the picture with every -update. A lock file beside
// it keeps two processes running the same test (two worktrees) apart; the folder goes when the
// test ends. Without /tmp it is a temporary folder.
func fixedDir(t testing.TB) string {
	t.Helper()
	name := strings.NewReplacer("/", "-", " ", "-").Replace(t.Name())
	root := filepath.Join("/tmp", "cogpit-tests")
	fixedMu.Lock()
	held := fixedHeld[name]
	fixedHeld[name] = true
	fixedMu.Unlock()
	if held || os.MkdirAll(root, 0o700) != nil {
		return t.TempDir()
	}
	lock, err := os.OpenFile(filepath.Join(root, name+".lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return t.TempDir()
	}
	if syscall.Flock(int(lock.Fd()), syscall.LOCK_EX) != nil {
		_ = lock.Close()
		return t.TempDir()
	}
	dir := filepath.Join(root, name)
	release := func() {
		_ = os.RemoveAll(dir)
		_ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
		_ = lock.Close()
		fixedMu.Lock()
		delete(fixedHeld, name)
		fixedMu.Unlock()
	}
	if os.RemoveAll(dir) != nil || os.Mkdir(dir, 0o700) != nil {
		release()
		return t.TempDir()
	}
	t.Cleanup(release)
	return dir
}
