package testkit

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	repo "helm.sh/helm/v4/pkg/repo/v1"
)

// Helm's repositories.yaml for tests (D-29): Cogpit reads the repositories the helm CLI keeps
// there. Tests write the file in a temporary folder with the Helm SDK's own types, as helm repo add
// writes it, and hand its path to engine.NewHelmRepos; no test resolves or reads the developer's
// own Helm files.

// HelmRepoFile is a temporary repositories.yaml.
type HelmRepoFile struct {
	// Path is the file's path, in a temporary folder of the test.
	Path string
	t    testing.TB
	n    int
}

// NewHelmRepoFile returns a repositories.yaml path in a temporary folder; the file is written by
// Write, not before.
func NewHelmRepoFile(t testing.TB) *HelmRepoFile {
	t.Helper()
	return &HelmRepoFile{Path: filepath.Join(t.TempDir(), "helm", "repositories.yaml"), t: t}
}

// Write replaces the file with these entries, atomically (a reader sees the old file or the new
// one), with a modification time that differs from the previous write's.
func (f *HelmRepoFile) Write(entries ...*repo.Entry) {
	f.t.Helper()
	rf := repo.NewFile()
	rf.Generated = chartModTime
	rf.Add(entries...)
	f.WriteRaw(nil, rf)
}

// WriteText replaces the file with text as it is (a file that does not parse, for one).
func (f *HelmRepoFile) WriteText(text string) {
	f.t.Helper()
	f.WriteRaw([]byte(text), nil)
}

// WriteRaw writes data, or rf through the SDK when data is nil.
func (f *HelmRepoFile) WriteRaw(data []byte, rf *repo.File) {
	f.t.Helper()
	if err := os.MkdirAll(filepath.Dir(f.Path), 0o700); err != nil {
		f.t.Fatal(err)
	}
	tmp := f.Path + ".tmp"
	if rf != nil {
		if err := rf.WriteFile(tmp, 0o600); err != nil {
			f.t.Fatal(err)
		}
	} else if err := os.WriteFile(tmp, data, 0o600); err != nil {
		f.t.Fatal(err)
	}
	f.n++
	at := time.Now().Add(time.Duration(f.n) * time.Second) // never the previous write's time
	if err := os.Chtimes(tmp, at, at); err != nil {
		f.t.Fatal(err)
	}
	// On Windows a rename over the file fails while Cogpit's poller has it open for a read: it runs
	// again for up to a second, as Cogpit's own renames do (engine's retryHeld).
	err := os.Rename(tmp, f.Path)
	for deadline := time.Now().Add(time.Second); err != nil && runtime.GOOS == "windows" && time.Now().Before(deadline); err = os.Rename(tmp, f.Path) {
		time.Sleep(5 * time.Millisecond)
	}
	if err != nil {
		f.t.Fatal(err)
	}
}

// Entry is an entry for a repository of the test, by name, without credentials.
func (r *HelmRepo) Entry(name string) *repo.Entry {
	return &repo.Entry{Name: name, URL: r.URL}
}
