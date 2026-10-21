// Package kubectl finds, checks and downloads the kubectl that Cogpit's terminals run
// (docs/developer/decisions.md D-3): the client version of a kubectl binary, the version skew
// Kubernetes supports between kubectl and an API server, the releases Cogpit downloaded, and the
// download of the release that matches a cluster from dl.k8s.io or a mirror.
//
// Layer L1. It talks to the release host and to sigstore's transparency log (Rekor), never to an
// API server, and runs a kubectl binary only as "kubectl version --client". A download is put in
// place only after it is verified: its SHA-256 against the published .sha256 file, and its
// signature (.sig) and signing certificate (.cert) with sigstore-go against the sigstore trusted
// root Cogpit embeds, the log's entry for them included (verify.go, rekor.go; decision D-50).
package kubectl

import (
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
)

// Version is a Kubernetes release: major, minor and patch. A provider's suffix ("-eks-1a2b3c",
// "+k3s1") and a prerelease are not part of it.
type Version struct {
	Major, Minor, Patch int
}

// versionRE reads "v1.36.1", "1.36.1", "v1.30.4+k3s1" and "v1.29.3-gke.1093000".
var versionRE = regexp.MustCompile(`^v?(\d{1,6})\.(\d{1,6})\.(\d{1,6})(?:[-+][0-9A-Za-z.+-]*)?$`)

// ParseVersion reads a Kubernetes version as an API server or kubectl reports it.
func ParseVersion(s string) (Version, error) {
	m := versionRE.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return Version{}, fmt.Errorf("%q is not a Kubernetes version such as v1.36.1", s)
	}
	n := func(s string) int { v, _ := strconv.Atoi(s); return v } // at most six digits: no overflow
	return Version{Major: n(m[1]), Minor: n(m[2]), Patch: n(m[3])}, nil
}

// String is the version without the leading v ("1.36.1"), the name of its directory under the
// directory for binaries.
func (v Version) String() string { return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch) }

// Tag is the release's tag ("v1.36.1"), as the release host's paths name it.
func (v Version) Tag() string { return "v" + v.String() }

// Compare orders versions: negative when v is older than o, positive when newer.
func (v Version) Compare(o Version) int {
	return cmp.Or(cmp.Compare(v.Major, o.Major), cmp.Compare(v.Minor, o.Minor), cmp.Compare(v.Patch, o.Patch))
}

// MaxMinorSkew is the version skew Kubernetes supports between kubectl and the API server: one
// minor version, older or newer (https://kubernetes.io/releases/version-skew-policy/#kubectl).
const MaxMinorSkew = 1

// Supported reports whether a kubectl of version client is supported against an API server of
// version server: the same major version, and at most MaxMinorSkew minor versions apart.
func Supported(client, server Version) bool {
	if client.Major != server.Major {
		return false
	}
	d := client.Minor - server.Minor
	return d >= -MaxMinorSkew && d <= MaxMinorSkew
}

// Matching is the kubectl release Cogpit offers for an API server: the server's own major, minor
// and patch, without a provider's suffix (an EKS "v1.36.1-eks-1a2b3c" gets kubectl 1.36.1).
func Matching(server Version) Version { return server }

// BinaryName is kubectl's file name on an operating system: kubectl.exe on Windows.
func BinaryName(goos string) string {
	if goos == "windows" {
		return "kubectl.exe"
	}
	return "kubectl"
}

// Installation is a kubectl release in the directory for binaries.
type Installation struct {
	Version Version
	Path    string // <dir>/<version>/kubectl
}

// Installed lists the kubectl releases in dir as Download leaves them, <dir>/<version>/kubectl,
// newest first. An entry whose name is not a version, or that holds no executable regular kubectl
// file, is skipped; a missing dir lists nothing. goos names the releases' platform (the binary's
// name); whether a file is executable is this system's matter, and Windows keeps no execute bit.
func Installed(dir, goos string) []Installation {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	name := BinaryName(goos)
	var out []Installation
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		v, err := ParseVersion(e.Name())
		if err != nil || v.String() != e.Name() { // only the names Download writes
			continue
		}
		path := filepath.Join(dir, e.Name(), name)
		fi, err := os.Stat(path)
		if err != nil || !fi.Mode().IsRegular() || (runtime.GOOS != "windows" && fi.Mode().Perm()&0o100 == 0) {
			continue
		}
		out = append(out, Installation{Version: v, Path: path})
	}
	slices.SortFunc(out, func(a, b Installation) int { return b.Version.Compare(a.Version) })
	return out
}

// Best picks the installed release to run against an API server of version server: the closest
// minor version (the same minor first), the newest among equals; ok is false when none is
// supported.
func Best(installed []Installation, server Version) (best Installation, ok bool) {
	dist := func(v Version) int {
		d := v.Minor - server.Minor
		if d < 0 {
			return -d
		}
		return d
	}
	for _, in := range installed {
		if !Supported(in.Version, server) {
			continue
		}
		if !ok || dist(in.Version) < dist(best.Version) ||
			(dist(in.Version) == dist(best.Version) && in.Version.Compare(best.Version) > 0) {
			best, ok = in, true
		}
	}
	return best, ok
}
