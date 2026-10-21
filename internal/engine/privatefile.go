package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/guided-traffic/cogpit/internal/model"
)

// ensurePrivateDir creates dir (and its parents) owner-only and checks it: a symlink or a
// non-directory is refused rather than written through, and a directory that other users can
// access is tightened to 0700, since it is Cogpit's own. On Windows, where modes do not express
// access, the user profile's ACLs apply.
func ensurePrivateDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	fi, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	switch {
	case fi.Mode()&fs.ModeSymlink != 0:
		return fmt.Errorf("%s is a symlink; Cogpit does not write through it", dir)
	case !fi.IsDir():
		return fmt.Errorf("%s is not a directory", dir)
	case runtime.GOOS != "windows" && fi.Mode().Perm() != 0o700:
		if err := os.Chmod(dir, 0o700); err != nil {
			return err
		}
	}
	return nil
}

// writePrivateFile replaces path with data atomically, owner-only: mode 0600 in a 0700 directory
// (ensurePrivateDir), written to a temporary file in the same directory, synced and renamed over
// path, so that a crash leaves the old or the new content, never a torn file. A symlink or other
// non-regular file at path is refused; rename replaces a symlink that appears after the check
// instead of following it.
func writePrivateFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := ensurePrivateDir(dir); err != nil {
		return err
	}
	switch fi, err := os.Lstat(path); {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return err
	case fi.Mode()&fs.ModeSymlink != 0:
		return fmt.Errorf("%s is a symlink; Cogpit does not write through it", path)
	case !fi.Mode().IsRegular():
		return fmt.Errorf("%s is not a regular file", path)
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+"-*.tmp") // created 0600
	if err != nil {
		return err
	}
	done := false
	defer func() {
		if !done {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
		}
	}()
	if err := tmp.Chmod(0o600); err != nil && runtime.GOOS != "windows" {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := renameFile(tmp.Name(), path); err != nil {
		return err
	}
	done = true
	syncDir(dir)
	return nil
}

// heldRetry bounds how long retryHeld runs a rename or a removal again on Windows.
const heldRetry = time.Second

// retryHeld runs op, a rename or a removal. On Windows those fail while another process has the
// file open without sharing its deletion (another Cogpit reading it, an editor, a virus scanner),
// which macOS and Linux allow. Such a hold lasts moments, so there op runs again for up to
// heldRetry, as in Go's own cmd/go (robustio), rather than the change being lost.
func retryHeld(op func() error) error {
	err := op()
	deadline := time.Now().Add(heldRetry)
	for wait := time.Millisecond; err != nil && heldOpenElsewhere(err) && time.Now().Before(deadline); wait = min(2*wait, 100*time.Millisecond) {
		time.Sleep(wait)
		err = op()
	}
	return err
}

// renameFile is os.Rename, run again on Windows while a file is held open elsewhere (retryHeld).
func renameFile(from, to string) error {
	return retryHeld(func() error { return os.Rename(from, to) })
}

// readPrivateJSON decodes the JSON file at path into v. A missing file leaves v alone and is not
// an error; a file larger than maxSize, a symlink or one that does not decode is.
func readPrivateJSON(path string, maxSize int64, v any) error {
	fi, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, maxSize+1))
	if err != nil {
		return err
	}
	if int64(len(data)) > maxSize {
		return fmt.Errorf("%s is larger than %s", path, model.FormatBytes(maxSize))
	}
	return json.Unmarshal(data, v)
}
