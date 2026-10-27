//go:build darwin && cgo

package nativedialog

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/testkit/prereq"
)

// harnessDir holds the harness the tests share; TestMain removes it.
var harnessDir string

func TestMain(m *testing.M) {
	code := m.Run()
	if harnessDir != "" {
		_ = os.RemoveAll(harnessDir)
	}
	os.Exit(code)
}

// errNoClang: the harness cannot be built here.
var errNoClang = errors.New("no clang")

// harnessBuild builds testdata/panel_harness.m with panel_darwin.m once per test binary.
var harnessBuild = sync.OnceValues(func() (string, error) {
	clang, err := exec.LookPath("clang")
	if err != nil {
		return "", errNoClang
	}
	if harnessDir, err = os.MkdirTemp("", "cogpit-panel-harness-*"); err != nil {
		return "", err
	}
	bin := filepath.Join(harnessDir, "harness")
	build := exec.Command(clang, "-fno-objc-arc", "-framework", "Cocoa", "-framework", "UniformTypeIdentifiers",
		"-I.", "-o", bin, filepath.Join("testdata", "panel_harness.m"), "panel_darwin.m")
	if out, err := build.CombinedOutput(); err != nil {
		return "", fmt.Errorf("%w\n%s", err, out)
	}
	return bin, nil
})

// buildHarness returns the harness, a process of its own: AppKit's panels need an NSApplication
// on the main thread, which a test binary's goroutines do not have.
func buildHarness(t *testing.T) string {
	t.Helper()
	bin, err := harnessBuild()
	prereq.Need(t, !errors.Is(err, errNoClang), "clang on PATH")
	if err != nil {
		t.Fatalf("building the harness: %v", err)
	}
	return bin
}

// runHarness runs the harness and returns its output.
func runHarness(t *testing.T, harness string, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	out, err := exec.CommandContext(ctx, harness, args...).Output()
	if err != nil {
		t.Fatalf("harness %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// fields parses the harness's line of tab-separated key=value words.
func fields(t *testing.T, line string) map[string]string {
	t.Helper()
	m := map[string]string{}
	for _, f := range strings.Split(strings.TrimRight(line, "\n"), "\t") {
		k, v, ok := strings.Cut(f, "=")
		if !ok {
			t.Fatalf("not a key=value word: %q in %q", f, line)
		}
		m[k] = v
	}
	return m
}

func b01(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

func openArgs(o OpenOptions) []string {
	return []string{"open", "title=" + o.Title, "message=" + o.Message, "prompt=" + o.Prompt, "dir=" + o.Dir,
		"types=" + strings.Join(o.Types, ","), "files=" + b01(o.Files), "folders=" + b01(o.Folders),
		"multiple=" + b01(o.Multiple), "hidden=" + b01(o.ShowHidden)}
}

func saveArgs(o SaveOptions) []string {
	return []string{"save", "title=" + o.Title, "message=" + o.Message, "prompt=" + o.Prompt, "dir=" + o.Dir,
		"types=" + strings.Join(o.Types, ","), "name=" + o.Name}
}

// The panels as built from the options the shell passes (panel_darwin.m's cogpitPanelMakeOpen
// and cogpitPanelMakeSave), read back from the configured, never shown panel in a process of its
// own: what may be chosen, several or one, hidden files, the button, the types, the folder it
// opens in; an open panel never offers New Folder, a save panel does and keeps the extension in
// sight. Skipped without clang.
func TestPanelConfiguration(t *testing.T) {
	harness := buildHarness(t)
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		args []string
		want map[string]string
	}{
		{"sync", openArgs(OpenOptions{Title: "Sync Files and Folders", Message: "Select kubeconfig files or folders of them.",
			Prompt: "Sync", Files: true, Folders: true, Multiple: true, ShowHidden: true, Dir: dir}),
			map[string]string{"kind": "open", "files": "1", "folders": "1", "multiple": "1", "hidden": "1", "create": "0",
				"types": "", "dir": dir, "prompt": "Sync", "title": "Sync Files and Folders",
				"message": "Select kubeconfig files or folders of them."}},
		{"folder", openArgs(OpenOptions{Prompt: "Choose", Folders: true, Dir: dir}),
			map[string]string{"kind": "open", "files": "0", "folders": "1", "multiple": "0", "hidden": "0", "prompt": "Choose", "dir": dir}},
		{"image", openArgs(OpenOptions{Files: true, Types: []string{"png", "jpg", "jpeg", "svg"}, Dir: dir}),
			map[string]string{"kind": "open", "files": "1", "folders": "0", "types": "public.png,public.jpeg,public.svg-image"}},
		{"yaml", saveArgs(SaveOptions{Name: "configmap-settings.yaml", Types: []string{"yaml", "yml"}, Dir: dir}),
			map[string]string{"kind": "save", "name": "configmap-settings.yaml", "types": "public.yaml", "create": "1",
				"extensionHidden": "0", "dir": dir}},
		{"csv", saveArgs(SaveOptions{Name: "pods.csv", Types: []string{"csv"}}),
			map[string]string{"kind": "save", "name": "pods.csv", "types": "public.comma-separated-values-text"}},
		{"log", saveArgs(SaveOptions{Name: "web-1.log"}),
			map[string]string{"kind": "save", "name": "web-1.log", "types": ""}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel() // each is a process of its own, which mostly waits for AppKit to start
			got := fields(t, runHarness(t, harness, tc.args...))
			for k, want := range tc.want {
				if got[k] != want {
					t.Errorf("%s = %q, want %q (all: %v)", k, got[k], want, got)
				}
			}
		})
	}
}

// A CA file's types: each extension gives a type that the panel accepts, declared or dynamic.
func TestPanelCATypes(t *testing.T) {
	harness := buildHarness(t)
	got := fields(t, runHarness(t, harness, openArgs(OpenOptions{Files: true, Types: []string{"pem", "crt", "cer"}})...))
	if types := strings.Split(got["types"], ","); got["types"] == "" || len(types) > 3 {
		t.Errorf("types = %q, want one to three types", got["types"])
	}
}

// A security-scoped bookmark of a file, made and resolved as the app makes and resolves it, in a
// process of its own: it resolves to the file, its access starts, and the file reads; a second
// resolution starts and stops accessing once more. Skipped without clang.
func TestBookmarkHarness(t *testing.T) {
	harness := buildHarness(t)
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "config")
	if err := os.WriteFile(file, []byte("apiVersion: v1"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := runHarness(t, harness, "bookmark", file)
	for _, want := range []string{"created 1", "resolved " + file, "accessing 1", "stale 0", "read apiVersion: v1", "stopped 1"} {
		if !strings.Contains(out, want+"\n") {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

// The Go side's bookmarks: a read-only one of a file and a read-write one of a folder resolve to
// their items and start accessing them; a file moved since gives a fresh bookmark (stale), which
// resolves to where it is now; garbage does not resolve.
func TestBookmarkRoundTrip(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "kubeconfig")
	if err := os.WriteFile(file, []byte("kind: Config"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		path   string
		access Access
	}{{file, ReadOnly}, {dir, ReadWrite}} {
		data, err := makeBookmark(tc.path, tc.access)
		if err != nil || len(data) == 0 {
			t.Fatalf("makeBookmark(%s) = %d bytes, %v", tc.path, len(data), err)
		}
		r, err := resolveBookmark(data, tc.access)
		if err != nil {
			t.Fatalf("resolveBookmark(%s): %v", tc.path, err)
		}
		if r.path != tc.path || !r.accessing || r.fresh != nil {
			t.Errorf("resolved %s to %+v, want its path, accessing, not stale", tc.path, r)
		}
	}

	data, err := makeBookmark(file, ReadOnly)
	if err != nil {
		t.Fatal(err)
	}
	moved := filepath.Join(dir, "moved")
	if err := os.Rename(file, moved); err != nil {
		t.Fatal(err)
	}
	r, err := resolveBookmark(data, ReadOnly)
	if err != nil {
		t.Fatalf("resolving a moved file's bookmark: %v", err)
	}
	if r.path != moved || len(r.fresh) == 0 {
		t.Fatalf("moved file resolved to %q, fresh %d bytes; want %q and a fresh bookmark", r.path, len(r.fresh), moved)
	}
	if again, err := resolveBookmark(r.fresh, ReadOnly); err != nil || again.path != moved || again.fresh != nil {
		t.Errorf("the fresh bookmark resolved to %+v, %v", again, err)
	}

	if _, err := resolveBookmark([]byte("not a bookmark"), ReadOnly); err == nil {
		t.Error("garbage resolved")
	}
}

// The store with the system's resolver: a kept bookmark survives a new run (a store opened again
// from its file) and is restored there.
func TestStoreRestoresRealBookmarks(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "dev.yaml")
	if err := os.WriteFile(file, []byte("kind: Config"), 0o600); err != nil {
		t.Fatal(err)
	}
	data, err := makeBookmark(file, ReadOnly)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config", BookmarksFile)
	s, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Keep(UseKubeconfig, file, ReadOnly, data); err != nil {
		t.Fatal(err)
	}
	next, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	rep, err := next.Restore(func(Entry) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	if rep.Restored != 1 || rep.Accessing != 1 || rep.Failed != 0 {
		t.Errorf("Restore = %+v, want one restored and accessing", rep)
	}
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("store file: %v, mode %v; want 0600", err, fi.Mode().Perm())
	}
	if strconv.Itoa(len(next.Entries())) != "1" {
		t.Errorf("entries = %v", next.Entries())
	}
}
