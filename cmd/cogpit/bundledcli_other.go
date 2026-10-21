//go:build !darwin

package main

// readOnlyVolume counts every path as read-only: only the macOS app bundle carries cogpit-cli in a
// bundle (D-16). The Linux package installs it as /usr/bin/cogpit-cli, which bundledCLI finds
// beside /usr/bin/cogpit (packagedCLIBeside, D-108); the Windows setup installs cogpit-cli.exe
// beside cogpit.exe, which a release's bundledCLI finds there (installedCLIBeside).
func readOnlyVolume(string) bool { return true }
