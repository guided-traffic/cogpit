//go:build windows

package engine

import (
	"errors"
	"syscall"
)

// errorSharingViolation is ERROR_SHARING_VIOLATION: another process has the file open without
// sharing the access asked for.
const errorSharingViolation = syscall.Errno(32)

// heldOpenElsewhere reports whether a file operation failed because another process has the file
// open: Windows says so with a sharing violation, and with access denied for a file that is being
// replaced or removed, or that a rename would replace.
func heldOpenElsewhere(err error) bool {
	return errors.Is(err, errorSharingViolation) || errors.Is(err, syscall.ERROR_ACCESS_DENIED)
}
