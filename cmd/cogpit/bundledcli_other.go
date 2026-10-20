//go:build !darwin

package main

// readOnlyVolume counts every path as read-only: only the macOS app bundle carries cogpit-cli in a
// bundle (D-16). The Linux package installs it as /usr/bin/cogpit-cli, which bundledCLI finds
// beside /usr/bin/cogpit (packagedCLIBeside, D-108); Windows packages come with M11.
func readOnlyVolume(string) bool { return true }
