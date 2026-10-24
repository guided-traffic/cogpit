package engine

import (
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/guided-traffic/cogpit/internal/cluster"
)

// Program folders (D-120, the owner's decision of 2026-10-23; H-6): the folders Cogpit looks in
// for the programs a kubeconfig's credential plugin runs (kubelogin, a cloud's plugin, kubectl run
// as the plugin and the kubectl plugin it runs, such as krew's kubectl-oidc_login), and for every
// other program it starts (a terminal's shell gets them on its PATH). A GUI app does not inherit
// the PATH a shell's profile sets; Cogpit appends its built-in folders at start
// (cluster.EnsureToolPath: Homebrew, MacPorts, Nix, krew, go install, asdf, mise and others), and
// these, the user's own, after them.
//
// They are a preference (Preferences.ProgramFolders) set in Preferences › Kubernetes through the
// system's folder panel (D-72), and take effect at once, for the next program started
// (cluster.SetProgramFolders): a failed login connected again finds what the new folder holds.
// Only the user adds a folder, in Cogpit's window (preferences.addProgramFolder is GUI-only,
// ErrProgramFoldersGUIOnly): a folder an agent could add would let it put a program of its choice
// where the user's next login looks for its plugin. Removing one is anyone's
// (preferences.removeProgramFolder): it can only make fewer programs found.
//
// Appended, never prepended: a program PATH or a built-in folder finds stays the one that runs, so a
// program folder never shadows a system program, nor the program a plugin's approval names (D-26
// binds a program on PATH by its name). A folder that is missing is skipped until it exists; one
// that every user of the computer may write to is refused when it is added and skipped if it
// becomes so (cluster.ProgramFolderProblem).

// ErrProgramFoldersGUIOnly refuses adding a program folder from anywhere but the user's own action
// in Cogpit's window.
var ErrProgramFoldersGUIOnly = errors.New("only the user, in Cogpit's own window, can add a folder Cogpit looks for programs in: " +
	"a folder an agent added could hold a program of its choice that the user's next login runs as its credential plugin; " +
	"agents and the CLI can read the folders and remove one")

// Bounds of the program folders.
const (
	MaxProgramFolders = 32
	maxProgramFolder  = 1024
)

// ErrProgramFolderKept is the answer to adding a folder that is a program folder already.
var ErrProgramFolderKept = errors.New("the folder is one of the program folders already")

// CheckProgramFolder says why a folder cannot be added as a program folder: not an absolute path of
// one line, missing, not a folder, or one every user of the computer may write to. It returns the
// folder cleaned.
func CheckProgramFolder(dir string) (string, error) {
	dir = strings.TrimSpace(dir)
	if err := checkProgramFolderPath(dir); err != nil {
		return "", err
	}
	dir = filepath.Clean(dir)
	if err := cluster.ProgramFolderProblem(dir); err != nil {
		return "", err
	}
	return dir, nil
}

// checkProgramFolderPath refuses a folder that is not an absolute path of one line.
func checkProgramFolderPath(dir string) error {
	switch {
	case dir == "":
		return errors.New("name the folder")
	case len(dir) > maxProgramFolder || !utf8.ValidString(dir) || strings.IndexFunc(dir, unicode.IsControl) >= 0:
		return fmt.Errorf("a program folder is one line of at most %d bytes", maxProgramFolder)
	case !filepath.IsAbs(dir):
		return fmt.Errorf("the folder %q: give its absolute path", dir)
	}
	return nil
}

// checkProgramFolders refuses a list Preferences cannot keep: more than MaxProgramFolders, a folder
// that is not an absolute path of one line, or one listed twice. Whether the folders exist is not
// checked here: one that went is kept and skipped until it is back.
func checkProgramFolders(dirs []string) error {
	if len(dirs) > MaxProgramFolders {
		return fmt.Errorf("at most %d program folders", MaxProgramFolders)
	}
	for i, d := range dirs {
		if err := checkProgramFolderPath(d); err != nil {
			return err
		}
		if slices.ContainsFunc(dirs[:i], func(e string) bool { return sameFolder(e, d) }) {
			return fmt.Errorf("the folder %s is listed twice", d)
		}
	}
	return nil
}

// normalProgramFolders keeps of folders read from the preferences file those checkProgramFolders
// takes: absolute paths of one line, cleaned, each once, at most MaxProgramFolders.
func normalProgramFolders(dirs []string) []string {
	var out []string
	for _, d := range dirs {
		if checkProgramFolderPath(d) != nil || len(out) == MaxProgramFolders {
			continue
		}
		d = filepath.Clean(d)
		if !slices.ContainsFunc(out, func(e string) bool { return sameFolder(e, d) }) {
			out = append(out, d)
		}
	}
	return out
}

// sameFolder reports whether two folders are the same entry of PATH: cleaned, and on Windows in
// any case.
func sameFolder(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == goosWindows {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// applyProgramFolders puts the folders on the process's PATH (cluster.SetProgramFolders).
func applyProgramFolders(dirs []string) { cluster.SetProgramFolders(dirs) }

// AddProgramFolder appends a folder to the program folders, checked (CheckProgramFolder), and puts
// it on PATH at once. ErrProgramFolderKept for one that is there already.
func (s *Session) AddProgramFolder(dir string) (Preferences, error) {
	dir, err := CheckProgramFolder(dir)
	if err != nil {
		return Preferences{}, err
	}
	return s.ChangePreferences(func(p *Preferences) error {
		if slices.ContainsFunc(p.ProgramFolders, func(e string) bool { return sameFolder(e, dir) }) {
			return ErrProgramFolderKept
		}
		if len(p.ProgramFolders) >= MaxProgramFolders {
			return fmt.Errorf("at most %d program folders", MaxProgramFolders)
		}
		p.ProgramFolders = append(p.ProgramFolders, dir)
		return nil
	})
}

// RemoveProgramFolder removes a folder from the program folders and from PATH at once; a folder
// that is not one is refused.
func (s *Session) RemoveProgramFolder(dir string) (Preferences, error) {
	dir = strings.TrimSpace(dir)
	return s.ChangePreferences(func(p *Preferences) error {
		i := slices.IndexFunc(p.ProgramFolders, func(e string) bool { return sameFolder(e, dir) })
		if i < 0 {
			return fmt.Errorf("%q is not one of the program folders", dir)
		}
		p.ProgramFolders = slices.Delete(p.ProgramFolders, i, i+1)
		return nil
	})
}

// ReadProgramFolders returns the program folders kept in the preferences file under dir (Cogpit's
// configuration directory), for a process that keeps no preferences of its own (cogpit-cli): it
// looks for programs where the app does. None when the file is missing or unreadable.
func ReadProgramFolders(dir string) []string {
	if dir == "" {
		return nil
	}
	doc := preferencesDoc{}
	if err := readPrefsDoc(filepath.Join(dir, PreferencesFile), &doc); err != nil {
		return nil
	}
	if doc.Version != 0 && doc.Version != preferencesVersion {
		return nil
	}
	return normalProgramFolders(doc.ProgramFolders)
}

// programFoldersHint is what the login stage of a connect adds when its credential plugin did not
// find a program (cluster.Stage.Missing): where to add the folder it is in.
func programFoldersHint(missing string) string {
	return "; if " + missing + " is installed, add the folder it is in under Preferences › Kubernetes › Program folders " +
		"(Cogpit does not read your shell's PATH)"
}
