package kubeconfig

import (
	"errors"
	"strings"
)

// MaxFileSize bounds a kubeconfig Cogpit reads from a source. Real kubeconfigs
// with many contexts and embedded certificates stay far below it.
const MaxFileSize = 1 << 20

// ErrNoContexts is returned for a kubeconfig document that defines no context: there is nothing
// to connect to, and a sync folder's other files (notes, backups) look like that.
var ErrNoContexts = errors.New("kubeconfig: the document defines no context")

// SyncCandidate reports whether a file of this name in a sync folder is read as a kubeconfig.
// Hidden files are (".kubeconfig", ".config"): Lens reads them (capture) and a user's ~/.kube holds
// some. What keeps a hidden or any other file harmless is not its name but the checks after it:
// only regular files (a FIFO or device is never opened), at most MaxFileSize bytes, and it must
// parse as a kubeconfig that defines a context. Editor backups, swap files, temporary downloads and
// the Finder's .DS_Store are skipped by name, as noise.
func SyncCandidate(name string) bool {
	switch {
	case name == "", name == ".DS_Store", strings.HasPrefix(name, "._"), strings.HasSuffix(name, "~"),
		strings.HasSuffix(name, ".swp"), strings.HasSuffix(name, ".tmp"):
		return false
	}
	return true
}
