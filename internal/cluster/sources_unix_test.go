//go:build unix

package cluster

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// TestSyncFolderNeverOpensAFIFO: a named pipe in a sync folder, hidden or not, is neither listed
// nor opened (opening one for reading blocks until a writer comes), and InspectFile refuses it.
// Hidden files are read; this is the check that keeps that harmless.
func TestSyncFolderNeverOpensAFIFO(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"pipe", ".hiddenpipe"} {
		if err := syscall.Mkfifo(filepath.Join(dir, name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, ".real"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		got, err := ListSyncFolder(dir)
		if err != nil || len(got) != 1 || filepath.Base(got[0].Path) != ".real" {
			t.Errorf("ListSyncFolder = %v, %v", got, err)
		}
		if _, err := InspectFile(filepath.Join(dir, ".hiddenpipe")); err == nil {
			t.Error("InspectFile read a FIFO")
		}
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("listing or inspecting blocked on a FIFO")
	}
}
