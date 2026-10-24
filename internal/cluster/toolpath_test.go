package cluster

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/testkit"
)

// isolateToolPath gives a test the tool path's state of a fresh process, with only the folders of
// builtin as the built-in ones, and the PATH it had back afterwards.
func isolateToolPath(t *testing.T, builtin ...string) {
	t.Helper()
	t.Setenv("PATH", os.Getenv("PATH")) // restored after the test, whatever Cogpit set
	saved := toolPath.Load()
	toolPath.Store(&toolPathState{folders: func() []string { return builtin }})
	t.Cleanup(func() { toolPath.Store(saved) })
}

func mkdirs(t *testing.T, dirs ...string) {
	t.Helper()
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func joinPath(parts ...string) string { return strings.Join(parts, string(os.PathListSeparator)) }

// composePath appends after PATH's own folders only: a folder PATH has keeps its place, a folder
// given twice (or once cleaned, once not) is appended once, a missing, relative or refused folder
// is skipped, and the order of the rest is kept.
func TestComposePathAppendsWithoutShadowing(t *testing.T) {
	root := t.TempDir()
	a, b, c := filepath.Join(root, "a"), filepath.Join(root, "b"), filepath.Join(root, "c")
	sys := filepath.Join(root, "sys")
	refused := filepath.Join(root, "refused")
	usable := func(d string) bool { return d != refused && !strings.HasSuffix(d, "missing") }

	path := joinPath(sys, b)
	got, added := composePath(path, []string{a, b, a + string(filepath.Separator), "relative/bin", "", refused,
		filepath.Join(root, "missing"), c, filepath.Join(c, "..", "c")}, usable)
	if want := joinPath(sys, b, a, c); got != want {
		t.Errorf("PATH = %q, want %q", got, want)
	}
	if !slices.Equal(added, []string{a, c}) {
		t.Errorf("added %q, want %q", added, []string{a, c})
	}
	if !strings.HasPrefix(got, path+string(os.PathListSeparator)) {
		t.Errorf("PATH's own folders did not stay in front: %q", got)
	}

	if got, added := composePath(path, []string{sys, b}, usable); got != path || added != nil {
		t.Errorf("nothing new: %q %q, want PATH unchanged", got, added)
	}
	if got, _ := composePath("", []string{a, c}, usable); got != joinPath(a, c) {
		t.Errorf("empty PATH: %q", got)
	}
	if runtime.GOOS == goosWindows {
		if got, _ := composePath(`C:\Tools`, []string{`c:\tools`}, usable); got != `C:\Tools` {
			t.Errorf("Windows compares folders in any case: %q", got)
		}
	}
}

// A folder that is missing, a file, or writable by every user of the computer is not one Cogpit
// looks for programs in.
func TestProgramFolderProblem(t *testing.T) {
	root := t.TempDir()
	ok := filepath.Join(root, "bin")
	file := filepath.Join(root, "file")
	open := filepath.Join(root, "open")
	mkdirs(t, ok, open)
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(open, 0o777|os.ModeSticky); err != nil {
		t.Fatal(err)
	}
	if err := ProgramFolderProblem(ok); err != nil {
		t.Errorf("a usable folder: %v", err)
	}
	for _, c := range []struct{ dir, want string }{
		{"bin", "not an absolute path"},
		{filepath.Join(root, "missing"), "does not exist"},
		{file, "is not a folder"},
	} {
		if err := ProgramFolderProblem(c.dir); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want %q", c.dir, err, c.want)
		}
	}
	err := ProgramFolderProblem(open)
	if runtime.GOOS == goosWindows {
		if err != nil {
			t.Errorf("Windows has no mode bits to read: %v", err)
		}
	} else if err == nil || !strings.Contains(err.Error(), "every user of this computer may write") {
		t.Errorf("a folder everyone may write to: %v", err)
	}
}

// The built-in folders: the package managers' and installers', krew's as its installer writes it
// (${KREW_ROOT:-$HOME/.krew}/bin), go install's ($GOBIN, else $GOPATH's first, else ~/go), none on
// Windows.
func TestBuiltinToolFolders(t *testing.T) {
	home := "/Users/ana"
	env := map[string]string{"USER": "ana"}
	getenv := func(k string) string { return env[k] }
	if got := BuiltinToolFolders("windows", `C:\Users\ana`, getenv); got != nil {
		t.Errorf("Windows: %q", got)
	}
	if runtime.GOOS == goosWindows {
		t.Skip("the folders of macOS and Linux, in their paths")
	}
	mac := BuiltinToolFolders("darwin", home, getenv)
	for _, want := range []string{"/opt/homebrew/bin", "/usr/local/bin", "/opt/local/bin", "/run/current-system/sw/bin",
		"/nix/var/nix/profiles/default/bin", "/etc/profiles/per-user/ana/bin", home + "/.nix-profile/bin", home + "/.local/bin",
		home + "/bin", home + "/.krew/bin", home + "/go/bin", home + "/.asdf/shims", home + "/.local/share/mise/shims",
		home + "/.rd/bin", home + "/.docker/bin"} {
		if !slices.Contains(mac, filepath.FromSlash(want)) {
			t.Errorf("macOS lacks %s: %q", want, mac)
		}
	}
	if slices.Contains(mac, "/snap/bin") {
		t.Error("snap's folder on macOS")
	}
	if i, j := slices.Index(mac, "/opt/homebrew/bin"), slices.Index(mac, filepath.FromSlash(home+"/.krew/bin")); i < 0 || j < i {
		t.Errorf("the package managers' folders come first: %q", mac)
	}
	linux := BuiltinToolFolders("linux", home, getenv)
	for _, want := range []string{"/home/linuxbrew/.linuxbrew/bin", "/snap/bin"} {
		if !slices.Contains(linux, want) {
			t.Errorf("Linux lacks %s", want)
		}
	}

	env = map[string]string{"KREW_ROOT": "/opt/krew", "GOPATH": joinPath("relative", "/srv/go", "/other"),
		"ASDF_DATA_DIR": "/data/asdf", "XDG_DATA_HOME": "/data/xdg", "USER": "../x"}
	got := BuiltinToolFolders("darwin", home, getenv)
	for _, want := range []string{"/opt/krew/bin", "/srv/go/bin", "/data/asdf/shims", "/data/xdg/mise/shims"} {
		if !slices.Contains(got, filepath.FromSlash(want)) {
			t.Errorf("lacks %s: %q", want, got)
		}
	}
	if slices.Contains(got, filepath.FromSlash(home+"/.krew/bin")) {
		t.Error("~/.krew/bin with KREW_ROOT set: krew uses KREW_ROOT alone")
	}
	for _, d := range got {
		if strings.Contains(d, "per-user") {
			t.Errorf("a user name with a separator made a folder: %s", d)
		}
	}
	env["GOBIN"] = "/srv/gobin"
	if got := BuiltinToolFolders("darwin", home, getenv); !slices.Contains(got, filepath.FromSlash("/srv/gobin")) || slices.Contains(got, filepath.FromSlash("/srv/go/bin")) {
		t.Errorf("GOBIN wins over GOPATH: %q", got)
	}
	if got := BuiltinToolFolders("darwin", "", getenv); slices.ContainsFunc(got, func(d string) bool { return strings.Contains(d, ".krew") }) {
		t.Errorf("no home: no home folders, %q", got)
	}
}

// EnsureToolPath appends the built-in folders that exist after PATH; the user's program folders
// come after them, at once, and go again when removed; a folder that appears later counts from the
// next refresh (a Connect); a PATH someone else set becomes the base; the same call twice changes
// nothing.
func TestToolPathEnsureAndProgramFolders(t *testing.T) {
	root := t.TempDir()
	sys, brew, krew := filepath.Join(root, "sys"), filepath.Join(root, "brew"), filepath.Join(root, "krew")
	mine, later := filepath.Join(root, "mine"), filepath.Join(root, "later")
	mkdirs(t, sys, brew, krew, mine)
	isolateToolPath(t, brew, filepath.Join(root, "absent"), krew, sys)
	t.Setenv("PATH", sys)

	EnsureToolPath()
	want := joinPath(sys, brew, krew)
	if runtime.GOOS == goosWindows {
		want = sys // no built-in folders on Windows
	}
	if got := os.Getenv("PATH"); got != want {
		t.Fatalf("after EnsureToolPath: %q, want %q", got, want)
	}
	EnsureToolPath()
	if got := os.Getenv("PATH"); got != want {
		t.Errorf("a second EnsureToolPath changed PATH to %q", got)
	}

	SetProgramFolders([]string{mine, later, sys}) // sys is PATH's: never twice
	if got := os.Getenv("PATH"); got != joinPath(want, mine) {
		t.Errorf("with the user's folders: %q, want %q", got, joinPath(want, mine))
	}
	mkdirs(t, later)
	RefreshToolPath()
	if got := os.Getenv("PATH"); got != joinPath(want, mine, later) {
		t.Errorf("a folder that appeared: %q", got)
	}
	SetProgramFolders([]string{later})
	if got := os.Getenv("PATH"); got != joinPath(want, later) {
		t.Errorf("one removed: %q", got)
	}
	SetProgramFolders(nil)
	if got := os.Getenv("PATH"); got != want {
		t.Errorf("all removed: %q, want %q", got, want)
	}

	other := filepath.Join(root, "other")
	t.Setenv("PATH", other)
	SetProgramFolders([]string{mine})
	if got := os.Getenv("PATH"); !strings.HasPrefix(got, other+string(os.PathListSeparator)) || !strings.HasSuffix(got, mine) {
		t.Errorf("a PATH set elsewhere is the new base: %q", got)
	}
}

// RefreshToolPath and SetProgramFolders with no folders leave a process alone that never composed
// its PATH: tests and tools that do not call EnsureToolPath keep theirs.
func TestToolPathUntouchedUntilUsed(t *testing.T) {
	isolateToolPath(t, "/opt/homebrew/bin")
	t.Setenv("PATH", "/only/this")
	RefreshToolPath()
	SetProgramFolders(nil)
	if got := os.Getenv("PATH"); got != "/only/this" {
		t.Errorf("PATH = %q", got)
	}
}

// MissingProgram reads client-go's "executable not found" for a command named without a folder,
// and kubectl's unknown command from a plugin's standard error, as the kubectl plugin it looked for.
func TestMissingProgram(t *testing.T) {
	for _, c := range []struct {
		err  error
		want string
	}{
		{errors.New(`Get "https://x/version": getting credentials: exec: executable kubelogin not found` +
			"\n\nIt looks like you are trying to use a client-go credential plugin that is not installed."), "kubelogin"},
		{errors.New(`getting credentials: exec: executable /opt/x/kubelogin not found`), ""},
		{errors.New(`getting credentials: exec: executable kubectl failed with exit code 1`), ""},
		{&PluginError{Err: errors.New("getting credentials: exec: executable kubectl failed with exit code 1"),
			Stderr: "error: unknown command \"oidc-login\" for \"kubectl\""}, "kubectl-oidc_login"},
		{&ConnectError{Problem: "x", Err: &PluginError{Err: errors.New("exec: executable kubectl failed with exit code 1"),
			Stderr: "error: unknown command \"get-token\" for \"kubectl\"\n\nDid you mean this?\n\tget"}}, "kubectl-get_token"},
		{&PluginError{Err: errors.New("exec: executable kubectl failed with exit code 1"), Stderr: "error: You must be logged in"}, ""},
		{nil, ""},
	} {
		if got := MissingProgram(c.err); got != c.want {
			t.Errorf("%v: %q, want %q", c.err, got, c.want)
		}
	}
}

// A credential plugin that is not found fails the login stage with the program it missed; once its
// folder is a program folder, the next Connect finds it, without a restart. kubectl run as the
// plugin, without the kubectl plugin it names, fails it with kubectl-<name>.
func TestConnectNamesTheMissingProgram(t *testing.T) {
	if runtime.GOOS == goosWindows {
		t.Skip("the test plugins are shell scripts")
	}
	srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{Token: secretToken})
	defer srv.Close()
	isolateToolPath(t)
	t.Setenv("PATH", "/usr/bin:/bin")
	EnsureToolPath()

	dir := t.TempDir()
	const name = "cogpit-test-login-0815"
	cred := `{"apiVersion":"client.authentication.k8s.io/v1","kind":"ExecCredential","status":{"token":"` + secretToken + `"}}`
	script := "#!/bin/sh\necho '" + cred + "'\n"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	path := writeFile(t, "kubeconfig", stderrKubeconfig(srv, name))

	_, _, err := traceConnect(t, path)
	login := stageNamed(t, err, StageLogin)
	if login.Result != StageFailed || login.Missing != name || !strings.Contains(login.Text, "no program "+name+" on PATH") {
		t.Errorf("login stage %+v", login)
	}

	SetProgramFolders([]string{dir})
	if _, _, err := traceConnect(t, path); err != nil {
		t.Fatalf("with its folder a program folder: %v", err)
	}

	kubectl := filepath.Join(t.TempDir(), "kubectl")
	stub := "#!/bin/sh\necho 'error: unknown command \"oidc-login\" for \"kubectl\"' >&2\nexit 1\n"
	if err := os.WriteFile(kubectl, []byte(stub), 0o700); err != nil {
		t.Fatal(err)
	}
	_, err = Connect(context.Background(), Options{KubeconfigPaths: []string{writeFile(t, "kubeconfig", stderrKubeconfig(srv, kubectl))},
		ConnectTimeout: 5 * time.Second})
	login = stageNamed(t, err, StageLogin)
	if login.Missing != "kubectl-oidc_login" || !strings.Contains(login.Text, "it found no kubectl-oidc_login on PATH") {
		t.Errorf("kubectl's plugin: %+v", login)
	}
}

func stageNamed(t *testing.T, err error, name string) Stage {
	t.Helper()
	if err == nil {
		t.Fatal("connected")
	}
	stages, _ := StagesOf(err)
	for _, st := range stages {
		if st.Name == name {
			return st
		}
	}
	t.Fatalf("no %s stage in %+v (%v)", name, stages, err)
	return Stage{}
}
