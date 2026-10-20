// Package buildinfo is the build's identity: the one source of the version that the About dialog,
// `cogpit-cli version`, the MCP server and the stdio bridge report.
//
// A release build sets the version with the linker: the Makefile's dist targets pass
// -X github.com/guided-traffic/cogpit/internal/buildinfo.version=<x.y.z>, the release pipeline
// with the tag's version. A binary built from a release tag without that flag (go install
// …@v0.7.0, a clean checkout of the tag) carries the tag as its module version, which Go writes
// into every binary, and reports it too. Every other build is a development build: it reports
// "dev" and the commit Go recorded (vcs.revision and vcs.modified, which go build writes inside a
// git checkout). Nothing asks git or the network at run time.
//
// Sparkle does not read it: the macOS updater compares the bundle's CFBundleVersion, which the
// release build sets to the same version (hack/macos/sparkle.sh), and a development bundle has no
// update key, so no updater.
//
// Layer L0: the standard library only, no I/O.
package buildinfo

import (
	"regexp"
	"runtime/debug"
)

// version is the release's version, set by the release build's linker flags; empty otherwise.
var version string

// Dev is the version a development build reports.
const Dev = "dev"

// commitLen is how many hex digits of a commit a development build shows.
const commitLen = 12

// releaseTag is a module version that is a release tag: no pseudo-version, no prerelease, no
// "+dirty" (Go's mark of a tree with changes).
var releaseTag = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)

// Info is what a binary knows of its build.
type Info struct {
	Version  string // the release's version without a "v" ("0.7.0"), or Dev
	Release  bool   // false: a development build
	Commit   string // the commit Go recorded, commitLen hex digits; "" when it recorded none (go run)
	Modified bool   // the tree had changes that were not committed
}

// Read returns this binary's build information.
func Read() Info {
	bi, _ := debug.ReadBuildInfo()
	return from(version, bi)
}

// Version returns the release's version, or Dev: what the MCP server and the bridge report.
func Version() string { return Read().Version }

// from derives the build's identity from the linker's version and Go's build information.
func from(linked string, bi *debug.BuildInfo) Info {
	info := Info{Version: linked, Release: linked != ""}
	if bi == nil {
		if !info.Release {
			info.Version = Dev
		}
		return info
	}
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			info.Commit = s.Value
			if len(info.Commit) > commitLen {
				info.Commit = info.Commit[:commitLen]
			}
		case "vcs.modified":
			info.Modified = s.Value == "true"
		}
	}
	if !info.Release && releaseTag.MatchString(bi.Main.Version) && !info.Modified {
		info.Version, info.Release = bi.Main.Version[1:], true
	}
	if !info.Release {
		info.Version = Dev
	}
	return info
}

// CommitText is a development build's commit as people write it: "0123456789ab", with "-dirty"
// when the tree had changes (git describe's mark); "" for a release or without a commit.
func (i Info) CommitText() string {
	if i.Release || i.Commit == "" {
		return ""
	}
	if i.Modified {
		return i.Commit + "-dirty"
	}
	return i.Commit
}

// String is the version with a development build's commit: "0.7.0", "dev 0123456789ab-dirty" or
// "dev".
func (i Info) String() string {
	if c := i.CommitText(); c != "" {
		return i.Version + " " + c
	}
	return i.Version
}

// ThreadChecksText is the build's mode of Fyne's thread check as About, Diagnostics and the app
// log show it (D-103): "on (safety)" or "off (speed)".
func ThreadChecksText() string {
	if FyneThreadChecks {
		return "on (safety)"
	}
	return "off (speed)"
}
