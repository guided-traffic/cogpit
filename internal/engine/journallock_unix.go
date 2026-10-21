//go:build darwin || linux || freebsd || netbsd || openbsd || dragonfly

package engine

import (
	"errors"
	"os"
	"syscall"
)

// The journals' locks are flock(2) locks: they belong to the open file, so two opens of one file
// exclude each other even within a process (tests play two processes that way), and the kernel
// drops them when the process ends. On a network file system the result depends on its server
// (Linux NFS emulates flock with byte-range locks); the configuration directory is local in the
// common case.

// tryLockFile takes an exclusive lock on f without waiting; errLockBusy when another open file
// holds it.
func tryLockFile(f *os.File) error {
	err := flock(f, syscall.LOCK_EX|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
		return errLockBusy
	}
	return err
}

// lockFile takes an exclusive lock on f, waiting for it.
func lockFile(f *os.File) error { return flock(f, syscall.LOCK_EX) }

// unlockFile releases f's lock.
func unlockFile(f *os.File) error { return flock(f, syscall.LOCK_UN) }

// createOwnerFile creates an owner file, owner-only; it fails when the file exists.
func createOwnerFile(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
}

func flock(f *os.File, how int) error {
	rc, err := f.SyscallConn()
	if err != nil {
		return err
	}
	var ferr error
	if err := rc.Control(func(fd uintptr) {
		for {
			ferr = syscall.Flock(int(fd), how)
			if !errors.Is(ferr, syscall.EINTR) {
				return
			}
		}
	}); err != nil {
		return err
	}
	return ferr
}
