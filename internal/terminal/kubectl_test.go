//go:build !windows

package terminal

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/testkit/prereq"
)

// fakeKubectl writes a kubectl that prints its name, so that a test sees which one ran.
func fakeKubectl(t *testing.T, dir, says string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "kubectl")
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho "+says+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestKubectlFirstEnv: the session's own directory holds a link to the chosen kubectl and leads
// PATH; for zsh, ZDOTDIR points at Cogpit's .zshenv and remembers the user's.
func TestKubectlFirstEnv(t *testing.T) {
	target := fakeKubectl(t, filepath.Join(t.TempDir(), "1.36.1"), "cogpit")
	env := []string{"PATH=/old:/usr/bin", "HOME=/h", "PATH=/usr/local/bin:/usr/bin", "ZDOTDIR=/h/.config/zsh"}

	dir, sh, err := kubectlFirst(target, "/bin/sh", env, false)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	bin := filepath.Join(dir, "bin")
	if len(sh.env) != 1 || sh.env[0] != "PATH="+bin+":/usr/local/bin:/usr/bin" || len(sh.args) != 0 || !sh.login {
		t.Errorf("sh: %+v", sh)
	}
	if link, err := os.Readlink(filepath.Join(bin, "kubectl")); err != nil || link != target {
		t.Errorf("link %q, %v", link, err)
	}
	if fi, _ := os.Stat(dir); fi.Mode().Perm() != 0o700 {
		t.Errorf("mode %o", fi.Mode().Perm())
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Errorf("sh gets a shell's folder: %v", entries)
	}

	// bash: started with --init-file, not as a login shell (A26); enforce adds its variable (A27).
	bdir, bash, err := kubectlFirst(target, "/bin/bash", env, true)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(bdir)
	rc := filepath.Join(bdir, "bash", "rc")
	if bash.login || len(bash.args) != 2 || bash.args[0] != "--init-file" || bash.args[1] != rc ||
		strings.Join(bash.env, " ") != "PATH="+filepath.Join(bdir, "bin")+":/usr/local/bin:/usr/bin COGPIT_KUBECTL_ENFORCE=1 COGPIT_KUBECTL_DIR="+filepath.Join(bdir, "bin") {
		t.Errorf("bash: %+v", bash)
	}
	if data, err := os.ReadFile(rc); err != nil || string(data) != bashrc {
		t.Errorf("bash rc: %v", err)
	}
	_, fish, _ := kubectlFirst(target, "/opt/homebrew/bin/fish", env, false)
	if !fish.login || len(fish.args) != 2 || fish.args[0] != "-C" || fish.args[1] != fishInit {
		t.Errorf("fish: %+v", fish)
	}

	zdir, zst, err := kubectlFirst(target, "/bin/zsh", env, false)
	zextra := zst.env
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(zdir)
	want := []string{"PATH=" + filepath.Join(zdir, "bin") + ":/usr/local/bin:/usr/bin", "ZDOTDIR=" + filepath.Join(zdir, "zsh"),
		"COGPIT_ZDOTDIR=/h/.config/zsh", "COGPIT_ZDOTDIR_SET=1", "COGPIT_KUBECTL_DIR=" + filepath.Join(zdir, "bin")}
	if strings.Join(zextra, "\n") != strings.Join(want, "\n") {
		t.Errorf("zsh:\n%s\nwant\n%s", strings.Join(zextra, "\n"), strings.Join(want, "\n"))
	}
	if data, err := os.ReadFile(filepath.Join(zdir, "zsh", ".zshenv")); err != nil || string(data) != zshenv {
		t.Errorf(".zshenv: %v", err)
	}
	_, unset, _ := kubectlFirst(target, "/usr/local/bin/zsh", []string{"PATH=/usr/bin"}, false)
	if !strings.Contains(strings.Join(unset.env, " "), "COGPIT_ZDOTDIR_SET=0") {
		t.Errorf("no ZDOTDIR: %q", unset.env)
	}

	for _, bad := range []string{"kubectl", filepath.Join(t.TempDir(), "missing"), t.TempDir()} {
		if d, _, err := kubectlFirst(bad, "/bin/sh", env, false); err == nil {
			os.RemoveAll(d)
			t.Errorf("kubectl %q accepted", bad)
		}
	}

	// localEnv: the kubectl entries come after opts.Env and before KUBECONFIG.
	full := localEnv([]string{"PATH=/from-opts"}, "/own:/user", "PATH=/k/bin:/from-opts")
	if p, _ := lastEnv(full, "PATH"); p != "/k/bin:/from-opts" {
		t.Errorf("PATH %q", p)
	}
	if k, _ := lastEnv(full, "KUBECONFIG"); k != "/own:/user" {
		t.Errorf("KUBECONFIG %q", k)
	}
}

// TestStartLocalKubectlFirst: in a login sh, the chosen kubectl leads the environment's PATH, and
// the session's directory is gone after close.
func TestStartLocalKubectlFirst(t *testing.T) {
	target := fakeKubectl(t, filepath.Join(t.TempDir(), "1.36.1"), "cogpit-kubectl")
	sess, out, home := startTestShell(t, LocalOptions{
		KubeconfigPaths: []string{writeUserKubeconfig(t)}, Context: "ctx-a", Kubectl: target,
	})
	script := `printf '%s\n' "$PATH" > path.txt; echo "__DONE$((40+2))__"` + "\n"
	if _, err := sess.Write([]byte(script)); err != nil {
		t.Fatal(err)
	}
	out.waitFor(t, "__DONE42__", 20*time.Second)
	path := strings.Split(readTrimmed(t, filepath.Join(home, "path.txt")), ":")
	var own string
	for _, p := range path {
		if strings.HasSuffix(p, "/bin") && strings.Contains(p, "cogpit-terminal-") {
			own = p
		}
	}
	if own == "" {
		t.Fatalf("PATH %q has no session directory", path)
	}
	// /bin/sh's profile may reorder PATH (macOS's path_helper): the entry must be there, and the
	// link must lead to the chosen kubectl.
	if link, err := os.Readlink(filepath.Join(own, "kubectl")); err != nil || link != target {
		t.Errorf("link %q, %v", link, err)
	}
	_ = sess.Close()
	waitClosed(t, sess.Done(), 10*time.Second, "session")
	if _, err := os.Stat(filepath.Dir(own)); !os.IsNotExist(err) {
		t.Errorf("the session's directory is still there: %v", err)
	}
}

// TestZshKubectlFirstAfterTheProfile starts a login zsh whose profile puts another kubectl first,
// as Homebrew's shellenv and macOS's path_helper do, and checks that kubectl is still Cogpit's
// choice at the prompt, that the user's own startup files ran from the user's ZDOTDIR, and that
// zsh's ZDOTDIR is the user's again.
func TestZshKubectlFirstAfterTheProfile(t *testing.T) {
	zsh := prereq.LookPath(t, "zsh")
	home := t.TempDir()
	userZdot := filepath.Join(home, ".config", "zsh")
	if err := os.MkdirAll(userZdot, 0o700); err != nil {
		t.Fatal(err)
	}
	shadow := filepath.Join(home, "shadow")
	fakeKubectl(t, shadow, "shadow-kubectl")
	files := map[string]string{
		".zshenv":   "export COGPIT_TEST_ZSHENV=ran\n",
		".zprofile": "path=(" + shadow + " $path)\n", // the profile puts its kubectl first
		".zshrc":    "PS1='$ '\nexport COGPIT_TEST_ZSHRC=ran\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(userZdot, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	target := fakeKubectl(t, filepath.Join(t.TempDir(), "1.36.1"), "cogpit-kubectl")
	sess, err := StartLocal(LocalOptions{
		Shell: zsh, Dir: home, KubeconfigPaths: []string{writeUserKubeconfig(t)}, Context: "ctx-a", Kubectl: target,
		Env: []string{"HOME=" + home, "ZDOTDIR=" + userZdot},
	})
	if err != nil {
		t.Fatal(err)
	}
	closeOnCleanup(t, sess)
	out := record(sess)
	script := strings.Join([]string{
		`kubectl > which.txt 2>&1`,
		`printf '%s\n' "$ZDOTDIR" > zdotdir.txt`,
		`printf '%s %s\n' "$COGPIT_TEST_ZSHENV" "$COGPIT_TEST_ZSHRC" > ran.txt`,
		`printf '%s\n' "${COGPIT_KUBECTL_DIR-unset}" "${COGPIT_ZDOTDIR-unset}" > leftovers.txt`,
		`echo "__DONE$((40+2))__"`,
	}, "\n") + "\n"
	if _, err := sess.Write([]byte(script)); err != nil {
		t.Fatal(err)
	}
	out.waitFor(t, "__DONE42__", 20*time.Second)
	if got := readTrimmed(t, filepath.Join(home, "which.txt")); got != "cogpit-kubectl" {
		t.Errorf("kubectl at the prompt is %q, want Cogpit's choice", got)
	}
	if got := readTrimmed(t, filepath.Join(home, "zdotdir.txt")); got != userZdot {
		t.Errorf("ZDOTDIR %q, want the user's %q", got, userZdot)
	}
	if got := readTrimmed(t, filepath.Join(home, "ran.txt")); got != "ran ran" {
		t.Errorf("the user's .zshenv and .zshrc: %q", got)
	}
	if got := readTrimmed(t, filepath.Join(home, "leftovers.txt")); got != "unset\nunset" {
		t.Errorf("Cogpit's variables left in the shell: %q", got)
	}
}

// shellKubectlScene starts shell with a home whose profile files are files and checks what kubectl
// runs at the prompt, after the profile put another kubectl first.
func shellKubectlScene(t *testing.T, shell string, files map[string]string, env []string, enforce bool) (home string) {
	script := "kubectl > which.txt 2>&1; echo \"$COGPIT_TEST_PROFILE ${COGPIT_KUBECTL_DIR-unset} ${COGPIT_KUBECTL_ENFORCE-unset}\" > ran.txt; echo __DONE$((40+2))__\n"
	if filepath.Base(shell) == "fish" {
		script = "kubectl > which.txt 2>&1; echo $COGPIT_TEST_PROFILE (set -q COGPIT_KUBECTL_DIR; and echo set; or echo unset) > ran.txt; echo __DONE(math 40+2)__\n"
	}
	t.Helper()
	home = t.TempDir()
	shadow := filepath.Join(home, "shadow")
	fakeKubectl(t, shadow, "shadow-kubectl")
	for name, content := range files {
		path := filepath.Join(home, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(strings.ReplaceAll(content, "SHADOW", shadow)), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	target := fakeKubectl(t, filepath.Join(t.TempDir(), "1.36.1"), "cogpit-kubectl")
	sess, err := StartLocal(LocalOptions{
		Shell: shell, Dir: home, KubeconfigPaths: []string{writeUserKubeconfig(t)}, Context: "ctx-a", Kubectl: target,
		Env: append([]string{"HOME=" + home, "XDG_CONFIG_HOME=" + filepath.Join(home, ".config")}, env...), EnforceKubectl: enforce,
	})
	if err != nil {
		t.Fatal(err)
	}
	closeOnCleanup(t, sess)
	out := record(sess)
	if _, err := sess.Write([]byte(script)); err != nil {
		t.Fatal(err)
	}
	out.waitFor(t, "__DONE42__", 20*time.Second)
	return home
}

// TestBashKubectlFirstAfterTheProfile (A26): a bash whose login profile puts another kubectl first
// still runs Cogpit's at the prompt; the profile ran; Cogpit's variables are gone. With enforce
// (A27), an alias the user's rc defines does not win either.
func TestBashKubectlFirstAfterTheProfile(t *testing.T) {
	bash := prereq.LookPath(t, "bash")
	files := map[string]string{".bash_profile": "export PATH=SHADOW:$PATH\nexport COGPIT_TEST_PROFILE=ran\nPS1='$ '\n"}
	home := shellKubectlScene(t, bash, files, nil, false)
	if got := readTrimmed(t, filepath.Join(home, "which.txt")); got != "cogpit-kubectl" {
		t.Errorf("kubectl at bash's prompt is %q, want Cogpit's choice", got)
	}
	if got := readTrimmed(t, filepath.Join(home, "ran.txt")); got != "ran unset unset" {
		t.Errorf("the profile and Cogpit's variables: %q", got)
	}
	files[".bash_profile"] += "alias kubectl=SHADOW/kubectl\n"
	home = shellKubectlScene(t, bash, files, nil, true)
	if got := readTrimmed(t, filepath.Join(home, "which.txt")); got != "cogpit-kubectl" {
		t.Errorf("enforced, with the user's alias: kubectl is %q", got)
	}
}

// TestZshKubectlEnforced (A27): with enforce, an alias in the user's .zshrc does not win over
// Cogpit's kubectl.
func TestZshKubectlEnforced(t *testing.T) {
	zsh := prereq.LookPath(t, "zsh")
	files := map[string]string{".zshrc": "PS1='$ '\nexport COGPIT_TEST_PROFILE=ran\nalias kubectl=SHADOW/kubectl\n"}
	home := shellKubectlScene(t, zsh, files, nil, true)
	if got := readTrimmed(t, filepath.Join(home, "which.txt")); got != "cogpit-kubectl" {
		t.Errorf("enforced zsh: kubectl is %q", got)
	}
	if got := readTrimmed(t, filepath.Join(home, "ran.txt")); got != "ran unset unset" {
		t.Errorf("the rc and Cogpit's variables: %q", got)
	}
}

// TestFishKubectlFirstAfterTheConfig (A26, A27): fish runs Cogpit's -C after its configuration.
func TestFishKubectlFirstAfterTheConfig(t *testing.T) {
	fish := prereq.LookPath(t, "fish")
	files := map[string]string{".config/fish/config.fish": "set -gx PATH SHADOW $PATH\nset -gx COGPIT_TEST_PROFILE ran\nalias kubectl SHADOW/kubectl\n"}
	// fish 4.1 and later query the terminal at start and wait ten seconds for an answer, which
	// Cogpit's widget gives and the test's recorder does not.
	home := shellKubectlScene(t, fish, files, []string{"fish_features=no-query-term"}, true)
	if got := readTrimmed(t, filepath.Join(home, "which.txt")); got != "cogpit-kubectl" {
		t.Errorf("fish: kubectl is %q", got)
	}
	if got := readTrimmed(t, filepath.Join(home, "ran.txt")); got != "ran unset" {
		t.Errorf("fish's config and Cogpit's variables: %q", got)
	}
}
