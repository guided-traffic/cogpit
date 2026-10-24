package cluster

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
)

// Where Cogpit finds the programs a kubeconfig's credential plugin runs, kubectl's plugins and a
// terminal's kubectl (H-6, D-120). A GUI app started from the Dock, Finder, a desktop launcher or
// the Start menu does not inherit the PATH a login shell's profile sets, so a credential plugin
// installed by a package manager or by krew is "not found" in Cogpit while it runs in a terminal
// (the owner's colleague's `kubectl oidc-login`, 2026-10-23: krew's kubectl-oidc_login in
// ~/.krew/bin; kubectl, which client-go runs, then answers `unknown command "oidc-login"`).
//
// Cogpit therefore appends two kinds of folders to its own PATH, after every folder PATH already
// has:
//
//   - the built-in folders (BuiltinToolFolders): those of the package managers and installers that
//     rely on a shell profile to put their folder on PATH, each only when it exists (EnsureToolPath,
//     at start, on macOS and Linux);
//   - the user's program folders (Preferences › Kubernetes › Program folders, SetProgramFolders),
//     on every platform, after the built-in ones, in the order the user added them.
//
// Appended, never prepended: a program PATH finds stays the one that runs, so no folder of Cogpit's
// or the user's can shadow a system program or one the user's PATH puts first (a credential
// plugin's approval binds a program on PATH by its name, D-26). A folder anyone on the computer may
// write to is skipped, as is a folder that is missing or not a folder. The process's PATH is
// composed again from the PATH Cogpit started with whenever the user's folders change and at every
// Connect (RefreshToolPath), so that a folder added in the Preferences, or one that appeared since
// (krew installed while Cogpit runs), counts from the next login without a restart. Programs that
// run already keep the PATH they started with.

// toolPathState is the process's PATH as Cogpit composes it. Guarded by mu.
type toolPathState struct {
	mu      sync.Mutex
	builtin bool     // EnsureToolPath ran: the built-in folders are appended
	user    []string // the user's program folders, as SetProgramFolders gave them
	base    string   // PATH before Cogpit appended to it
	set     string   // PATH as Cogpit last left it
	applied bool     // set and base are known
	folders func() []string
}

// toolPath is the process's; tests put one of their own in its place.
var toolPath = func() *atomic.Pointer[toolPathState] {
	p := new(atomic.Pointer[toolPathState])
	p.Store(&toolPathState{folders: builtinToolFoldersHere})
	return p
}()

// EnsureToolPath appends the built-in tool folders (BuiltinToolFolders) that exist and PATH does
// not have yet, after PATH's own. Call it once at start-up, before the first Connect; it changes
// the process environment. On Windows there are none (a program started from the Start menu has
// the PATH a terminal has: both read it from the user's environment in the registry), and only the
// user's program folders are appended.
func EnsureToolPath() {
	st := toolPath.Load()
	st.mu.Lock()
	defer st.mu.Unlock()
	st.builtin = runtime.GOOS != goosWindows
	st.applyLocked()
}

// SetProgramFolders sets the user's program folders (Preferences › Kubernetes › Program folders,
// D-120): appended to PATH after its own folders and the built-in ones, those that exist, are
// folders and nobody but their owner may write to. It takes effect at once for every program
// started afterwards. The same folders again change nothing, unless PATH was set elsewhere since.
func SetProgramFolders(dirs []string) {
	st := toolPath.Load()
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.applied && slices.Equal(dirs, st.user) && os.Getenv("PATH") == st.set {
		return
	}
	st.user = slices.Clone(dirs)
	st.applyLocked()
}

// RefreshToolPath composes PATH again, as at start: a built-in or program folder that appeared
// since is appended, one that went is dropped. Connect calls it before it loads the kubeconfig;
// it does nothing before EnsureToolPath or SetProgramFolders first ran.
func RefreshToolPath() {
	st := toolPath.Load()
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.applied {
		st.applyLocked()
	}
}

// applyLocked sets PATH to its base with the folders appended. A PATH that someone else set since
// Cogpit last did (a test, nothing in the app) becomes the base. st.mu is held.
func (st *toolPathState) applyLocked() {
	cur := os.Getenv("PATH")
	if !st.applied || cur != st.set {
		st.base = cur
	}
	st.applied = true
	var extra []string
	if st.builtin {
		extra = st.folders()
	}
	extra = append(extra, st.user...)
	path, added := composePath(st.base, extra, usableToolFolder)
	if path == cur {
		st.set = cur
		return
	}
	if err := os.Setenv("PATH", path); err != nil {
		slog.Warn("cluster: cannot extend PATH", "err", err)
		st.set = cur
		return
	}
	st.set = path
	slog.Info("cluster: PATH composed", "appended", added)
}

// composePath returns path with the folders of extra appended that usable accepts and that path, or
// an earlier folder of extra, does not have yet (the same folder cleaned; on Windows in any case),
// and the folders it appended. Relative folders are skipped. path's own folders keep their order
// and their places in front: an appended folder never comes before one of them.
func composePath(path string, extra []string, usable func(string) bool) (string, []string) {
	have := map[string]bool{}
	for _, d := range filepath.SplitList(path) {
		if d != "" {
			have[pathKey(d)] = true
		}
	}
	var added []string
	for _, d := range extra {
		if d == "" || !filepath.IsAbs(d) {
			continue
		}
		k := pathKey(d)
		if have[k] || !usable(d) {
			continue
		}
		have[k] = true
		added = append(added, filepath.Clean(d))
	}
	if len(added) == 0 {
		return path, nil
	}
	parts := added
	if path != "" {
		parts = append([]string{path}, added...)
	}
	return strings.Join(parts, string(os.PathListSeparator)), added
}

// pathKey is a folder as PATH's lookup compares it: cleaned, and on Windows in lower case.
func pathKey(d string) string {
	d = filepath.Clean(d)
	if runtime.GOOS == goosWindows {
		d = strings.ToLower(d)
	}
	return d
}

// usableToolFolder reports whether a folder may be appended: it exists, is a folder (a link to one
// counts) and others may not write to it (ProgramFolderProblem).
func usableToolFolder(dir string) bool { return ProgramFolderProblem(dir) == nil }

// ProgramFolderProblem says why a folder cannot be one Cogpit looks for programs in: not an
// absolute path, missing, not a folder, or one that every user of the computer may write to (on
// macOS and Linux its mode grants others write access, sticky or not), where anyone could put a
// program for Cogpit to run. Nil for a usable folder.
func ProgramFolderProblem(dir string) error {
	if !filepath.IsAbs(dir) {
		return fmt.Errorf("the folder %q is not an absolute path", dir)
	}
	fi, err := os.Stat(dir)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return fmt.Errorf("the folder %s does not exist", dir)
	case err != nil:
		return fmt.Errorf("the folder %s cannot be read: %w", dir, errors.Unwrap(err))
	case !fi.IsDir():
		return fmt.Errorf("%s is not a folder", dir)
	case runtime.GOOS != goosWindows && fi.Mode().Perm()&0o002 != 0:
		return fmt.Errorf("every user of this computer may write to the folder %s, so anyone could put a program there for Cogpit to run", dir)
	}
	return nil
}

// goosWindows is runtime.GOOS on Windows.
const goosWindows = "windows"

// builtinToolFoldersHere are the built-in folders for this process: its system, home folder, user
// name and environment.
func builtinToolFoldersHere() []string {
	home, _ := os.UserHomeDir()
	return BuiltinToolFolders(runtime.GOOS, home, os.Getenv)
}

// BuiltinToolFolders are the folders EnsureToolPath appends after PATH on goos, for the home folder
// home and the environment getenv reads, in the order they are searched (D-120): those of the
// package managers and installers that put kubectl, credential plugins or kubectl plugins in a
// folder of their own and rely on a shell profile to put it on PATH. Folders that do not exist are
// skipped when PATH is composed. None on Windows.
func BuiltinToolFolders(goos, home string, getenv func(string) string) []string {
	if goos == goosWindows {
		return nil
	}
	dirs := []string{
		"/opt/homebrew/bin", // Homebrew on Apple silicon
		"/usr/local/bin",    // Homebrew on Intel Macs; installers (aws, Docker Desktop's links)
		"/opt/local/bin",    // MacPorts
	}
	if goos == "linux" {
		dirs = append(dirs,
			"/home/linuxbrew/.linuxbrew/bin", // Homebrew on Linux
			"/snap/bin",                      // snap's kubectl, helm
		)
	}
	// Nix: the system's profile (NixOS, nix-darwin), the multi-user default profile, the user's
	// profile of nix-darwin or home-manager.
	dirs = append(dirs, "/run/current-system/sw/bin", "/nix/var/nix/profiles/default/bin")
	if user := getenv("USER"); user != "" && !strings.ContainsAny(user, `/\`) {
		dirs = append(dirs, "/etc/profiles/per-user/"+user+"/bin")
	}
	if home == "" || !filepath.IsAbs(home) {
		return dirs
	}
	in := func(env, def string) string {
		if v := getenv(env); v != "" && filepath.IsAbs(v) {
			return v
		}
		return def
	}
	dataHome := in("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	return append(dirs,
		filepath.Join(home, ".nix-profile", "bin"), // the user's Nix profile
		filepath.Join(home, ".local", "bin"),       // pipx, the user's own (XDG)
		filepath.Join(home, "bin"),                 // the user's own
		// krew, kubectl's plugin manager: as its installer's PATH line, ${KREW_ROOT:-$HOME/.krew}/bin.
		filepath.Join(in("KREW_ROOT", filepath.Join(home, ".krew")), "bin"),
		// go install: $GOBIN, else the first folder of $GOPATH, else ~/go, as the go command picks it.
		in("GOBIN", filepath.Join(goPath(home, getenv), "bin")),
		// The version managers' shims: asdf's, mise's.
		filepath.Join(in("ASDF_DATA_DIR", filepath.Join(home, ".asdf")), "shims"),
		filepath.Join(in("MISE_DATA_DIR", filepath.Join(dataHome, "mise")), "shims"),
		// Rancher Desktop's kubectl and helm; Docker Desktop's tools installed per user.
		filepath.Join(home, ".rd", "bin"),
		filepath.Join(home, ".docker", "bin"),
	)
}

// goPath is the folder go install writes to without $GOBIN: the first absolute folder of $GOPATH,
// else ~/go.
func goPath(home string, getenv func(string) string) string {
	for _, d := range filepath.SplitList(getenv("GOPATH")) {
		if d != "" && filepath.IsAbs(d) {
			return d
		}
	}
	return filepath.Join(home, "go")
}

// What a credential plugin that was not found looks like (D-120). client-go's own message for a
// program it cannot start (k8s.io/client-go v0.37.1, plugin/pkg/client/auth/exec/exec.go:553):
// "exec: executable <command> not found", followed once by its hint at the documentation. kubectl,
// run as the plugin (`kubectl oidc-login get-token …`), without the kubectl plugin it names on its
// PATH: cobra's `error: unknown command "oidc-login" for "kubectl"` on its standard error, exit
// code 1; kubectl looks the plugin up as kubectl-<name>, its dashes as underscores.
var (
	execNotFound   = regexp.MustCompile(`exec: executable (.+?) not found`)
	kubectlUnknown = regexp.MustCompile(`unknown command "([^"\s]+)" for "kubectl"`)
)

// MissingProgram returns the program a credential plugin's failed login did not find, when the
// folders Cogpit looks in could have held it: the plugin's own command, named without a folder
// ("kubelogin"), that could not be started; or the kubectl plugin kubectl, run as the plugin, did
// not find ("kubectl-oidc_login", read from the plugin's standard error, PluginError.Stderr). Empty
// for any other failure, and for a command given as a path, which is not looked up on PATH.
func MissingProgram(err error) string {
	if err == nil {
		return ""
	}
	if m := execNotFound.FindStringSubmatch(err.Error()); m != nil {
		if strings.ContainsAny(m[1], `/\`) {
			return ""
		}
		return m[1]
	}
	var pe *PluginError
	if errors.As(err, &pe) {
		if m := kubectlUnknown.FindStringSubmatch(pe.Stderr); m != nil {
			return "kubectl-" + strings.ReplaceAll(m[1], "-", "_")
		}
	}
	return ""
}
