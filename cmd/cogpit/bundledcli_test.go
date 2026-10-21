package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// fakeBundle makes <dir>/<name>.app/Contents/MacOS with the app's executable and, unless cliMode
// is 0, a cogpit-cli with that mode; it returns the executable's path.
func fakeBundle(t *testing.T, dir, name string, cliMode os.FileMode) string {
	t.Helper()
	macOS := filepath.Join(dir, name+".app", "Contents", "MacOS")
	if err := os.MkdirAll(macOS, 0o755); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(macOS, "cogpit")
	if err := os.WriteFile(exe, []byte("app"), 0o755); err != nil { // #nosec G306 -- a test's fake executable
		t.Fatal(err)
	}
	if cliMode != 0 {
		if err := os.WriteFile(filepath.Join(macOS, bundledCLIName), []byte("cli"), cliMode); err != nil {
			t.Fatal(err)
		}
	}
	return exe
}

func resolved(t *testing.T, p string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func writable(string) bool { return false }

// TestBundledCLIBeside: the cogpit-cli beside the executable of an installed bundle, by the path
// the running executable has; none for a dev build, a bundle without it, one translocated or on a
// read-only volume, or a cogpit-cli that is not an executable file.
func TestBundledCLIBeside(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a macOS bundle's cogpit-cli is found by its executable bit, which Windows files lack; TestInstalledCLIBeside covers Windows")
	}
	dir := t.TempDir()

	exe := fakeBundle(t, dir, "Cogpit", 0o755)
	want := resolved(t, filepath.Join(filepath.Dir(exe), bundledCLIName))
	if got := bundledCLIBeside(exe, writable); got != want {
		t.Errorf("installed bundle: %q, want %q", got, want)
	}

	// A bundle whose folder has a space, reached through a symbolic link: the link's target.
	spaced := fakeBundle(t, filepath.Join(dir, "My Apps"), "Cogpit", 0o755)
	link := filepath.Join(dir, "link")
	if err := os.Symlink(filepath.Dir(filepath.Dir(filepath.Dir(spaced))), link); err != nil {
		t.Fatal(err)
	}
	if got, want := bundledCLIBeside(filepath.Join(link, "Contents", "MacOS", "cogpit"), writable),
		resolved(t, filepath.Join(filepath.Dir(spaced), bundledCLIName)); got != want {
		t.Errorf("through a link: %q, want %q", got, want)
	}

	for name, exe := range map[string]string{
		"no cogpit-cli":       fakeBundle(t, dir, "Old", 0),
		"not executable":      fakeBundle(t, dir, "NoExec", 0o644),
		"translocated":        fakeBundle(t, filepath.Join(dir, "AppTranslocation", "1234", "d"), "Cogpit", 0o755),
		"not a bundle":        writeDevBuild(t, filepath.Join(dir, "bin")),
		"missing executable":  filepath.Join(dir, "Gone.app", "Contents", "MacOS", "cogpit"),
		"MacOS not in .app":   fakeBundleNoApp(t, filepath.Join(dir, "plain")),
		"cogpit-cli a folder": fakeBundleCLIDir(t, filepath.Join(dir, "dir")),
	} {
		if got := bundledCLIBeside(exe, writable); got != "" {
			t.Errorf("%s: %q, want none", name, got)
		}
	}

	if got := bundledCLIBeside(exe, func(string) bool { return true }); got != "" {
		t.Errorf("read-only volume: %q, want none", got)
	}
}

// writeDevBuild is bin/cogpit with bin/cogpit-cli beside it, as make build leaves them.
func writeDevBuild(t *testing.T, bin string) string {
	t.Helper()
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"cogpit", bundledCLIName} {
		if err := os.WriteFile(filepath.Join(bin, n), []byte(n), 0o755); err != nil { // #nosec G306 -- a test's fake executable
			t.Fatal(err)
		}
	}
	return filepath.Join(bin, "cogpit")
}

// fakeBundleNoApp is Contents/MacOS in a folder whose name does not end in .app.
func fakeBundleNoApp(t *testing.T, dir string) string {
	t.Helper()
	fakeBundle(t, dir, "Cogpit", 0o755)
	plain := filepath.Join(dir, "Cogpit")
	if err := os.Rename(filepath.Join(dir, "Cogpit.app"), plain); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(plain, "Contents", "MacOS", "cogpit")
}

// fakeBundleCLIDir is a bundle whose cogpit-cli is a directory.
func fakeBundleCLIDir(t *testing.T, dir string) string {
	t.Helper()
	exe := fakeBundle(t, dir, "Cogpit", 0)
	if err := os.Mkdir(filepath.Join(filepath.Dir(exe), bundledCLIName), 0o755); err != nil {
		t.Fatal(err)
	}
	return exe
}

// TestPackagedCLIBeside: the Linux package's cogpit-cli beside its /usr/bin/cogpit, by the path the
// running executable has, also once apt replaced it under the process; none for an executable
// elsewhere (a dev build), without cogpit-cli, or with one that is not an executable file.
func TestPackagedCLIBeside(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the Linux package's cogpit-cli is found by its executable bit, which Windows files lack; TestInstalledCLIBeside covers Windows")
	}
	dir := resolved(t, t.TempDir())
	exe := filepath.Join(dir, "cogpit")
	cli := filepath.Join(dir, bundledCLIName)
	if err := os.WriteFile(exe, []byte("app"), 0o755); err != nil { // #nosec G306 -- a test's fake executable
		t.Fatal(err)
	}
	if got := packagedCLIBeside(exe, dir); got != "" {
		t.Errorf("without cogpit-cli: %q", got)
	}
	if err := os.WriteFile(cli, []byte("cli"), 0o644); err != nil { // #nosec G306 -- a test's fake file
		t.Fatal(err)
	}
	if got := packagedCLIBeside(exe, dir); got != "" {
		t.Errorf("cogpit-cli not executable: %q", got)
	}
	if err := os.Chmod(cli, 0o755); err != nil { // #nosec G302 -- a test's fake executable
		t.Fatal(err)
	}
	for _, e := range []string{exe, exe + " (deleted)"} {
		if got := packagedCLIBeside(e, dir); got != cli {
			t.Errorf("packagedCLIBeside(%q) = %q, want %q", e, got, cli)
		}
	}
	other := filepath.Join(t.TempDir(), "bin", "cogpit")
	if got := packagedCLIBeside(other, dir); got != "" {
		t.Errorf("an executable elsewhere: %q", got)
	}
}

// TestInstalledCLIBeside: the Windows setup's cogpit-cli.exe beside its cogpit.exe, in whatever
// folder the user chose, its name in any case; no executable bit is asked for, so the test runs on
// every system. None for an executable of another name, without cogpit-cli.exe, or with a folder
// of that name.
func TestInstalledCLIBeside(t *testing.T) {
	dir := resolved(t, t.TempDir())
	exe := filepath.Join(dir, "cogpit.exe")
	cli := filepath.Join(dir, bundledCLIName+".exe")
	if err := os.WriteFile(exe, []byte("app"), 0o644); err != nil { // #nosec G306 -- a test's fake program
		t.Fatal(err)
	}
	if got := installedCLIBeside(exe); got != "" {
		t.Errorf("without cogpit-cli.exe: %q", got)
	}
	if err := os.WriteFile(cli, []byte("cli"), 0o644); err != nil { // #nosec G306 -- a test's fake program
		t.Fatal(err)
	}
	for _, e := range []string{exe, filepath.Join(dir, "Cogpit.EXE")} {
		if got := installedCLIBeside(e); got != cli {
			t.Errorf("installedCLIBeside(%q) = %q, want %q", e, got, cli)
		}
	}
	if got := installedCLIBeside(filepath.Join(dir, "cogpit")); got != "" {
		t.Errorf("an executable without .exe: %q", got)
	}

	other := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(filepath.Join(other, bundledCLIName+".exe"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := installedCLIBeside(filepath.Join(other, "cogpit.exe")); got != "" {
		t.Errorf("cogpit-cli.exe a folder: %q", got)
	}
}
