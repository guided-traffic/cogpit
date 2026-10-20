package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// bundledCLIName is the command-line tool `make app` puts beside the app's own executable, in
// Cogpit.app/Contents/MacOS, where Apple's "Placing Content in a Bundle" puts helper tools (D-16).
const bundledCLIName = "cogpit-cli"

// bundledCLI returns the path of the cogpit-cli that came with the running app: the one in an
// installed macOS bundle (bundledCLIBeside), or the Linux package's beside /usr/bin/cogpit
// (packagedCLIBeside, D-108); "" otherwise: a dev build (bin/cogpit, go run) and every other
// system offer the HTTP registration instead.
func bundledCLI() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	switch runtime.GOOS {
	case "darwin":
		return bundledCLIBeside(exe, readOnlyVolume)
	case "linux":
		return packagedCLIBeside(exe, packagedBinDir)
	}
	return ""
}

// packagedBinDir is where the Linux package installs both binaries (D-108).
const packagedBinDir = "/usr/bin"

// packagedCLIBeside returns <dir>/cogpit-cli when exe is <dir>/cogpit, the Linux package's app (its
// symbolic links resolved; with " (deleted)" once apt replaced the file under the running process),
// and the cogpit-cli there is an executable file; "" otherwise.
func packagedCLIBeside(exe, dir string) string {
	exe = strings.TrimSuffix(exe, " (deleted)")
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	if exe != filepath.Join(dir, "cogpit") {
		return ""
	}
	cli := filepath.Join(dir, bundledCLIName)
	fi, err := os.Lstat(cli)
	if err != nil || !fi.Mode().IsRegular() || fi.Mode().Perm()&0o111 == 0 {
		return ""
	}
	return cli
}

// bundledCLIBeside returns the path of the cogpit-cli in the app bundle whose main executable is
// exe, or "" when there is none to register: exe is not <name>.app/Contents/MacOS/<exe>, the bundle
// runs translocated (a quarantined copy macOS moved to a random path for this run) or from a
// read-only volume (its disk image), whose paths are gone once the app quits or the image is
// ejected, or the bundle carries no executable cogpit-cli (a bundle built before it did). The path
// is the running executable's own, its symbolic links resolved; it is never guessed.
func bundledCLIBeside(exe string, readOnly func(string) bool) string {
	exe, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return ""
	}
	macOS := filepath.Dir(exe)
	contents := filepath.Dir(macOS)
	bundle := filepath.Dir(contents)
	if filepath.Base(macOS) != "MacOS" || filepath.Base(contents) != "Contents" || filepath.Ext(bundle) != ".app" {
		return ""
	}
	if strings.Contains(bundle, "/AppTranslocation/") || readOnly(bundle) {
		return ""
	}
	cli := filepath.Join(macOS, bundledCLIName)
	fi, err := os.Lstat(cli)
	if err != nil || !fi.Mode().IsRegular() || fi.Mode().Perm()&0o111 == 0 {
		return ""
	}
	return cli
}
