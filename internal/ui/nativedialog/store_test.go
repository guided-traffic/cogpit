package nativedialog

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// fakeResolver answers for bookmarks by their bytes: "gone" fails, "stale" gives a fresh one,
// anything else resolves.
func fakeResolver(calls *[]string) func([]byte, Access) (resolved, error) {
	return func(data []byte, _ Access) (resolved, error) {
		*calls = append(*calls, string(data))
		switch string(data) {
		case "gone":
			return resolved{}, errors.New("the file does not exist")
		case "stale":
			return resolved{path: "/moved", accessing: true, fresh: []byte("fresh")}, nil
		}
		return resolved{path: "/x", accessing: true}, nil
	}
}

func newTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "Cogpit", BookmarksFile)
	s, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	s.now = func() time.Time { return time.Date(2026, 10, 16, 23, 0, 0, 0, time.UTC) }
	return s, path
}

// Keep stores a bookmark under its use and path (cleaned, so the fixtures' slashes are backslashes
// on Windows), owner-only, replacing the one of the same use and path; nothing without a bookmark;
// a nil store keeps nothing.
func TestStoreKeep(t *testing.T) {
	s, path := newTestStore(t)
	if err := s.Keep(UseKubeconfig, "/Users/me/Downloads/dev.yaml", ReadOnly, []byte("one")); err != nil {
		t.Fatal(err)
	}
	if err := s.Keep(UseKubeconfig, "/Users/me/Downloads/./dev.yaml", ReadOnly, []byte("two")); err != nil {
		t.Fatal(err)
	}
	if err := s.Keep(UseFolder, "/Users/me/Downloads/dev.yaml", ReadWrite, []byte("three")); err != nil {
		t.Fatal(err)
	}
	if err := s.Keep(UseCAFile, "/ca.pem", ReadOnly, nil); err != nil {
		t.Fatal(err)
	}
	got := s.Entries()
	if len(got) != 2 || string(got[0].Bookmark) != "two" || got[0].Path != filepath.FromSlash("/Users/me/Downloads/dev.yaml") ||
		got[1].Use != UseFolder || got[1].Access != ReadWrite {
		t.Fatalf("entries = %+v", got)
	}
	again, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Entries()) != 2 {
		t.Errorf("reopened: %+v", again.Entries())
	}
	if runtime.GOOS != "windows" {
		fi, err := os.Stat(path)
		if err != nil || fi.Mode().Perm() != 0o600 {
			t.Errorf("file mode %v, %v; want 0600", fi.Mode().Perm(), err)
		}
		di, err := os.Stat(filepath.Dir(path))
		if err != nil || di.Mode().Perm() != 0o700 {
			t.Errorf("directory mode %v, %v; want 0700", di.Mode().Perm(), err)
		}
	}
	var none *Store
	if err := none.Keep(UseKubeconfig, "/a", ReadOnly, []byte("x")); err != nil || none.Entries() != nil {
		t.Errorf("a nil store kept something: %v", err)
	}
	if rep, err := none.Restore(nil); err != nil || rep != (Report{}) {
		t.Errorf("a nil store restored %+v, %v", rep, err)
	}
}

// Restore drops what keep refuses, resolves the rest, replaces a stale bookmark by the fresh one
// and keeps one that does not resolve (a volume not mounted resolves again later), and writes the
// store when it changed.
func TestStoreRestore(t *testing.T) {
	s, path := newTestStore(t)
	for i, b := range []string{"ok", "stale", "gone", "removed"} {
		if err := s.Keep(UseKubeconfig, fmt.Sprintf("/k/%d-%s", i, b), ReadOnly, []byte(b)); err != nil {
			t.Fatal(err)
		}
	}
	next, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	var calls []string
	next.resolve = fakeResolver(&calls)
	rep, err := next.Restore(func(e Entry) bool { return e.Path != filepath.FromSlash("/k/3-removed") })
	if err != nil {
		t.Fatal(err)
	}
	if want := (Report{Restored: 2, Accessing: 2, Refreshed: 1, Dropped: 1, Failed: 1}); rep != want {
		t.Errorf("Restore = %+v, want %+v", rep, want)
	}
	if fmt.Sprint(calls) != "[ok stale gone]" {
		t.Errorf("resolved %v: a dropped bookmark is not resolved", calls)
	}
	after, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	var paths, data []string
	for _, e := range after.Entries() {
		paths, data = append(paths, filepath.ToSlash(e.Path)), append(data, string(e.Bookmark))
	}
	if fmt.Sprint(paths) != "[/k/0-ok /k/1-stale /k/2-gone]" || fmt.Sprint(data) != "[ok fresh gone]" {
		t.Errorf("kept %v with %v", paths, data)
	}
}

// In a build without bookmarks Restore resolves nothing and only drops what keep refuses.
func TestStoreRestoreUnsupported(t *testing.T) {
	s, _ := newTestStore(t)
	_ = s.Keep(UseCAFile, "/ca.pem", ReadOnly, []byte("x"))
	_ = s.Keep(UseCAFile, "/old.pem", ReadOnly, []byte("y"))
	s.resolve = func([]byte, Access) (resolved, error) { return resolved{}, ErrUnsupported }
	rep, err := s.Restore(func(e Entry) bool { return e.Path != filepath.FromSlash("/old.pem") })
	if err != nil {
		t.Fatal(err)
	}
	if rep != (Report{Dropped: 1}) || len(s.Entries()) != 1 {
		t.Errorf("Restore = %+v, entries %+v", rep, s.Entries())
	}
}

// The store is bounded: the newest bookmarks of each use are kept, sixteen of a folder's or a CA
// file's, 256 of the kubeconfig sources'.
func TestStoreBounds(t *testing.T) {
	s, _ := newTestStore(t)
	for i := range maxOtherBookmarks + 3 {
		if err := s.Keep(UseFolder, fmt.Sprintf("/f/%02d", i), ReadWrite, []byte("b")); err != nil {
			t.Fatal(err)
		}
	}
	for i := range 5 {
		_ = s.Keep(UseKubeconfig, fmt.Sprintf("/k/%d", i), ReadOnly, []byte("b"))
	}
	got := s.Entries()
	if len(got) != maxOtherBookmarks+5 || got[0].Path != filepath.FromSlash("/f/03") {
		t.Fatalf("kept %d, first %q; want the newest %d folders and every source", len(got), got[0].Path, maxOtherBookmarks)
	}
	many := make([]Entry, 0, maxKubeconfigBookmarks+1)
	for i := range maxKubeconfigBookmarks + 1 {
		many = append(many, Entry{Use: UseKubeconfig, Path: fmt.Sprint(i)})
	}
	if capped := capEntries(many); len(capped) != maxKubeconfigBookmarks || capped[0].Path != "1" {
		t.Errorf("capped to %d from %q", len(capped), capped[0].Path)
	}
}

// A store file that does not load gives an empty store and the error; the next Keep replaces it.
// A symlink in its place is refused, not followed.
func TestStoreUnreadable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, BookmarksFile)
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := OpenStore(path)
	if err == nil || len(s.Entries()) != 0 {
		t.Fatalf("OpenStore = %v, %+v; want an error and no entries", err, s.Entries())
	}
	if err := s.Keep(UseKubeconfig, "/a", ReadOnly, []byte("x")); err != nil {
		t.Fatal(err)
	}
	if again, err := OpenStore(path); err != nil || len(again.Entries()) != 1 {
		t.Errorf("after Keep: %v, %+v", err, again.Entries())
	}
	if runtime.GOOS == "windows" {
		return
	}
	link := filepath.Join(dir, "link.json")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenStore(link); err == nil {
		t.Error("a symlinked store was read")
	}
}

// A store without a file keeps its bookmarks for the run.
func TestStoreWithoutFile(t *testing.T) {
	s, err := OpenStore("")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Keep(UseKubeconfig, "/a", ReadOnly, []byte("x")); err != nil || len(s.Entries()) != 1 {
		t.Errorf("Keep = %v, %+v", err, s.Entries())
	}
}
