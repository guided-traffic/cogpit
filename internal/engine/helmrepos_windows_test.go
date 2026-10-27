//go:build windows

package engine

import (
	"syscall"
	"testing"
	"time"
)

// holdExclusive opens path sharing nothing, as a program renaming the file into place holds it for
// a moment: until the returned func closes it, Windows refuses every other open of the file.
func holdExclusive(t *testing.T, path string) func() {
	t.Helper()
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	fd, err := syscall.CreateFile(name, syscall.GENERIC_READ, 0, nil, syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	return func() { _ = syscall.CloseHandle(fd) }
}

// Helm's file held by another program for a moment is read once the hold ends: the read runs
// again within heldRetry, and the list shows the file, not the refused open.
func TestHelmRepoFileHeldForAMomentIsRead(t *testing.T) {
	r := webRepo(t)
	h, f, _ := helmReposOn(t, r)
	f.Write(r.Entry("test"))
	time.AfterFunc(100*time.Millisecond, holdExclusive(t, f.Path))
	mustSync(t, h)
	if l, _ := h.List(); l.Status != HelmFileOK || len(l.Repositories) != 1 {
		t.Errorf("listed %+v", l)
	}
}
