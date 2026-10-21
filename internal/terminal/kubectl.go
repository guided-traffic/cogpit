package terminal

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// A local terminal can be told which kubectl to find first (LocalOptions.Kubectl, D-3): the
// version Cogpit downloaded for the cluster, or the one the Preferences name. The session gets a
// directory of its own, removed when it closes:
//
//	<dir>/bin/kubectl   a link to that kubectl, so that nothing else from its folder joins PATH
//	<dir>/zsh/.zshenv   for zsh only (below)
//
// and PATH=<dir>/bin:<PATH> in the shell's environment. A login shell's profile runs after that and
// can reorder PATH: macOS's /etc/zprofile and /etc/profile run path_helper, which moves every
// inherited entry behind /etc/paths and /etc/paths.d (a kubectl in /usr/local/bin, Docker
// Desktop's link, then wins), and a profile can prepend its own folders (Homebrew's shellenv).
// For zsh, the shell macOS starts by default, Cogpit sets ZDOTDIR to <dir>/zsh: zsh then reads
// Cogpit's .zshenv first, which gives zsh the user's own ZDOTDIR back, reads the user's .zshenv,
// and registers a hook that puts <dir>/bin first again before the first prompt, after every
// profile ran, and then removes itself. Every other file zsh reads is the user's, from the user's
// ZDOTDIR, as without Cogpit. For bash (A26, owner 2026-10-14 evening) Cogpit starts bash with
// --init-file <dir>/bash/rc instead of as a login shell: the rc reads the files a login bash reads
// (/etc/profile, then the first of ~/.bash_profile, ~/.bash_login and ~/.profile), then puts
// <dir>/bin first; bash then is no login shell for `shopt login_shell` and reads no ~/.bash_logout
// (the way VS Code's terminal starts bash, inferred to be the common pattern). For fish, Cogpit
// passes -C, which fish runs after its configuration. Other shells keep the environment's order
// only (docs/security/trust-boundaries.md, H-36).
//
// With LocalOptions.EnforceKubectl (A27, Preferences › Kubernetes) each of the three also defines
// a function named kubectl that runs <dir>/bin/kubectl, after removing an alias of that name, so
// that a folder the user puts first later, or an alias, does not win over Cogpit's choice.
//
// On Windows (localpty_windows.go, kubectlOnPath) a symbolic link needs a privilege: a kubectl
// named kubectl.exe has its own folder put first on PATH instead, and only one by another name
// gets the directory above, with a hard link or a copy named kubectl.exe. PowerShell and cmd keep
// the environment's order, and EnforceKubectl has no effect there.

// zshenv is the .zshenv of a zsh terminal whose kubectl Cogpit chose.
const zshenv = `# Written by Cogpit for one terminal and removed when it closes. zsh reads it in place of your
# .zshenv because Cogpit set ZDOTDIR: it gives zsh your own ZDOTDIR back, reads your .zshenv, and
# puts the kubectl Cogpit chose for this terminal first on PATH before the first prompt, after
# your profile ran.
if [[ ${COGPIT_ZDOTDIR_SET-} == 1 ]]; then
  ZDOTDIR=$COGPIT_ZDOTDIR
else
  unset ZDOTDIR
fi
unset COGPIT_ZDOTDIR COGPIT_ZDOTDIR_SET
if [[ -f ${ZDOTDIR:-$HOME}/.zshenv ]]; then
  source ${ZDOTDIR:-$HOME}/.zshenv
fi
cogpit_kubectl_first() {
  if [[ -n ${COGPIT_KUBECTL_DIR-} && -d $COGPIT_KUBECTL_DIR ]]; then
    path=("$COGPIT_KUBECTL_DIR" ${path:#$COGPIT_KUBECTL_DIR})
    if [[ ${COGPIT_KUBECTL_ENFORCE-} == 1 ]]; then
      unalias kubectl 2>/dev/null
      eval "kubectl() { ${(q)COGPIT_KUBECTL_DIR}/kubectl \"\$@\"; }"
    fi
  fi
  unset COGPIT_KUBECTL_DIR COGPIT_KUBECTL_ENFORCE
  precmd_functions=(${precmd_functions:#cogpit_kubectl_first})
  unfunction cogpit_kubectl_first
}
typeset -ga precmd_functions
precmd_functions+=(cogpit_kubectl_first)
`

// bashrc is the --init-file of a bash terminal whose kubectl Cogpit chose (A26).
const bashrc = `# Written by Cogpit for one terminal and removed when it closes. bash runs it in place of
# ~/.bashrc because Cogpit started bash with --init-file: it reads the files a login bash reads,
# then puts the kubectl Cogpit chose for this terminal first on PATH, after your profile ran.
if [ -f /etc/profile ]; then . /etc/profile; fi
for cogpit_f in ~/.bash_profile ~/.bash_login ~/.profile; do
  if [ -f "$cogpit_f" ]; then . "$cogpit_f"; break; fi
done
unset cogpit_f
if [ -n "${COGPIT_KUBECTL_DIR-}" ] && [ -d "$COGPIT_KUBECTL_DIR" ]; then
  cogpit_p=":$PATH:"
  cogpit_p="${cogpit_p//:$COGPIT_KUBECTL_DIR:/:}"
  cogpit_p="${cogpit_p#:}"
  cogpit_p="${cogpit_p%:}"
  PATH="$COGPIT_KUBECTL_DIR${cogpit_p:+:$cogpit_p}"
  unset cogpit_p
  if [ "${COGPIT_KUBECTL_ENFORCE-}" = 1 ]; then
    unalias kubectl 2>/dev/null
    eval "kubectl() { $(printf '%q' "$COGPIT_KUBECTL_DIR/kubectl") \"\$@\"; }"
  fi
fi
unset COGPIT_KUBECTL_DIR COGPIT_KUBECTL_ENFORCE
`

// fishInit is fish's -C command for a terminal whose kubectl Cogpit chose (A26): fish runs it
// after its configuration.
const fishInit = `if set -q COGPIT_KUBECTL_DIR; and test -d "$COGPIT_KUBECTL_DIR"; ` +
	`set -gx PATH $COGPIT_KUBECTL_DIR (string match -v -- $COGPIT_KUBECTL_DIR $PATH); ` +
	`if test "$COGPIT_KUBECTL_ENFORCE" = 1; functions -e kubectl; set -g __cogpit_kubectl $COGPIT_KUBECTL_DIR/kubectl; ` +
	`function kubectl; $__cogpit_kubectl $argv; end; end; end; set -e COGPIT_KUBECTL_DIR; set -e COGPIT_KUBECTL_ENFORCE`

// kubectlStart is how a shell is started so that Cogpit's kubectl comes first: its environment
// entries, the arguments that go before the user's, and whether it runs as a login shell.
type kubectlStart struct {
	env   []string
	args  []string
	login bool
}

// kubectlFirst prepares the session's own directory for opts.Kubectl and returns it with how the
// shell starts so that it comes first; env is the shell's environment before them (its last PATH
// and ZDOTDIR count); enforce defines the kubectl function (A27). The caller removes the
// directory when the session ends.
func kubectlFirst(kubectl, shellPath string, env []string, enforce bool) (dir string, start kubectlStart, err error) {
	if !filepath.IsAbs(kubectl) {
		return "", kubectlStart{}, fmt.Errorf("local terminal: kubectl %q is not an absolute path", kubectl)
	}
	if fi, err := os.Stat(kubectl); err != nil || !fi.Mode().IsRegular() {
		return "", kubectlStart{}, fmt.Errorf("local terminal: kubectl %s is not a file", kubectl)
	}
	dir, err = os.MkdirTemp("", "cogpit-terminal-*") // owner-only (0700)
	if err != nil {
		return "", kubectlStart{}, fmt.Errorf("local terminal: %w", err)
	}
	fail := func(err error) (string, kubectlStart, error) {
		_ = os.RemoveAll(dir)
		return "", kubectlStart{}, fmt.Errorf("local terminal: preparing kubectl: %w", err)
	}
	bin := filepath.Join(dir, "bin")
	if strings.ContainsRune(bin, os.PathListSeparator) {
		return fail(fmt.Errorf("the temporary directory %s contains %q and cannot be put on PATH", bin, os.PathListSeparator))
	}
	if err := os.Mkdir(bin, 0o700); err != nil {
		return fail(err)
	}
	if err := linkKubectl(kubectl, filepath.Join(bin, binaryName())); err != nil {
		return fail(err)
	}
	path, _ := lastEnv(env, "PATH")
	if path != "" {
		path = string(os.PathListSeparator) + path
	}
	start = kubectlStart{env: []string{"PATH=" + bin + path}, login: true}
	if enforce {
		start.env = append(start.env, "COGPIT_KUBECTL_ENFORCE=1")
	}
	switch filepath.Base(shellPath) {
	case "zsh":
		zdot := filepath.Join(dir, "zsh")
		if err := os.Mkdir(zdot, 0o700); err != nil {
			return fail(err)
		}
		if err := os.WriteFile(filepath.Join(zdot, ".zshenv"), []byte(zshenv), 0o600); err != nil {
			return fail(err)
		}
		set := "0"
		user, ok := lastEnv(env, "ZDOTDIR")
		if ok {
			set = "1"
		}
		start.env = append(start.env, "ZDOTDIR="+zdot, "COGPIT_ZDOTDIR="+user, "COGPIT_ZDOTDIR_SET="+set, "COGPIT_KUBECTL_DIR="+bin)
	case "bash":
		rc := filepath.Join(dir, "bash", "rc")
		if err := os.Mkdir(filepath.Dir(rc), 0o700); err != nil {
			return fail(err)
		}
		if err := os.WriteFile(rc, []byte(bashrc), 0o600); err != nil {
			return fail(err)
		}
		start.env = append(start.env, "COGPIT_KUBECTL_DIR="+bin)
		start.args, start.login = []string{"--init-file", rc}, false
	case "fish":
		start.env = append(start.env, "COGPIT_KUBECTL_DIR="+bin)
		start.args = []string{"-C", fishInit}
	}
	return dir, start, nil
}

// binaryName is kubectl's file name on this computer.
func binaryName() string {
	if os.PathSeparator == '\\' {
		return "kubectl.exe"
	}
	return "kubectl"
}

// lastEnv returns the last value of key in env, as os/exec passes a duplicated key; on Windows,
// whose names ignore case (its PATH is usually Path), in any case.
func lastEnv(env []string, key string) (string, bool) {
	for i := len(env) - 1; i >= 0; i-- {
		if k, v, ok := strings.Cut(env[i], "="); ok && (k == key || (os.PathSeparator == '\\' && strings.EqualFold(k, key))) {
			return v, true
		}
	}
	return "", false
}
