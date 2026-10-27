//go:build windows

package engine

import (
	"errors"
	"os"
	"syscall"
	"unsafe"
)

// The journals' locks are LockFileEx locks on the first byte: they belong to the handle, so two
// opens of one file exclude each other even within a process, and Windows releases them when the
// process ends. Go opens files non-inheritable, so child processes do not keep them. Built and
// vetted by CI on Windows; its tests first ran there on 2026-10-21, before the first Windows
// release (D-7).

var (
	kernel32         = syscall.NewLazyDLL("kernel32.dll")
	procLockFileEx   = kernel32.NewProc("LockFileEx")
	procUnlockFileEx = kernel32.NewProc("UnlockFileEx")
)

const (
	lockfileFailImmediately = 0x1
	lockfileExclusiveLock   = 0x2
	errorLockViolation      = syscall.Errno(33)
	errorIOPending          = syscall.Errno(997)
)

// tryLockFile takes an exclusive lock on f without waiting; errLockBusy when another handle
// holds it.
func tryLockFile(f *os.File) error {
	err := lockFileEx(f, lockfileExclusiveLock|lockfileFailImmediately)
	if errors.Is(err, errorLockViolation) || errors.Is(err, errorIOPending) {
		return errLockBusy
	}
	return err
}

// lockFile takes an exclusive lock on f, waiting for it.
func lockFile(f *os.File) error { return lockFileEx(f, lockfileExclusiveLock) }

// createOwnerFile creates an owner file; it fails when the file exists. Unlike os.OpenFile, it
// shares the file's deletion: the process holds the file open for its whole life, and Windows
// refuses to remove a file held open without that share, so the folder it is in could not be
// removed while Cogpit runs (a test's temporary folder, the user's own cleanup) as it can on
// macOS and Linux. Whether the owner lives stays a matter of the lock, as there. The handle is
// not inherited (no security attributes), as os.OpenFile's.
func createOwnerFile(path string) (*os.File, error) {
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	h, err := syscall.CreateFile(name, syscall.GENERIC_READ|syscall.GENERIC_WRITE,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE, nil,
		syscall.CREATE_NEW, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	return os.NewFile(uintptr(h), path), nil
}

// unlockFile releases f's lock.
func unlockFile(f *os.File) error {
	var ol syscall.Overlapped
	// #nosec G103 -- the address of ol for the duration of the call.
	r, _, err := procUnlockFileEx.Call(f.Fd(), 0, 1, 0, uintptr(unsafe.Pointer(&ol)))
	if r == 0 {
		return err
	}
	return nil
}

func lockFileEx(f *os.File, flags uint32) error {
	var ol syscall.Overlapped
	// #nosec G103 -- the address of ol for the duration of the call.
	r, _, err := procLockFileEx.Call(f.Fd(), uintptr(flags), 0, 1, 0, uintptr(unsafe.Pointer(&ol)))
	if r == 0 {
		return err
	}
	return nil
}
