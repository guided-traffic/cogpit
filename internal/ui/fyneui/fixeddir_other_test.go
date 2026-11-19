//go:build !unix

package fyneui

import "testing"

// fixedDir is a temporary folder where /tmp is not: the pictures are written on macOS and Linux
// (fixeddir_unix_test.go).
func fixedDir(t testing.TB) string { return t.TempDir() }
