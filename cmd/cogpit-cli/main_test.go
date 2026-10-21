package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/app"
	"github.com/guided-traffic/cogpit/internal/buildinfo"
)

// syncBuffer is a bytes.Buffer safe for the engine's goroutines that log.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// testEnv is a command environment of temporary directories: a home without anything in it and
// Cogpit's configuration directory under it. $HOME, $XDG_CONFIG_HOME and $KUBECONFIG point there
// too, and on Windows %USERPROFILE%, %APPDATA% and %LOCALAPPDATA%, so that nothing a test runs can
// reach the developer's ~/.kube or Cogpit's real files.
type testEnv struct {
	*env
	home   string
	out    *syncBuffer
	errOut *syncBuffer
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
	t.Setenv("KUBECONFIG", os.DevNull)
	out, errOut := &syncBuffer{}, &syncBuffer{}
	return &testEnv{
		env: &env{
			stdin: io.NopCloser(strings.NewReader("")), stdout: out, stderr: errOut,
			logger: slog.New(slog.NewTextHandler(errOut, &slog.HandlerOptions{Level: slog.LevelWarn})),
			level:  new(slog.LevelVar),
			dirs:   app.Dirs{Config: filepath.Join(home, "config", app.DirName), Home: home},
			demo:   testDemo(), // demo_test.go; a release build has none (nodemo_test.go)
		},
		home: home, out: out, errOut: errOut,
	}
}

// run runs a command line and returns its exit code.
func (te *testEnv) run(t *testing.T, args ...string) int {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	return run(ctx, args, te.env)
}

// writeKube writes a kubeconfig into the test home's ~/.kube, the default sync folder.
func (te *testEnv) writeKube(t *testing.T, name, content string) string {
	t.Helper()
	dir := filepath.Join(te.home, ".kube")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestHelpVersionAndUsageErrors(t *testing.T) {
	cases := []struct {
		args        []string
		code        int
		out, errOut string
	}{
		{args: nil, code: exitUsage, errOut: "Commands:"},
		{args: []string{"--help"}, code: exitOK, out: "contexts"},
		{args: []string{"help", "contexts"}, code: exitOK, out: "Usage:\n  cogpit-cli contexts"},
		{args: []string{"contexts", "-h"}, code: exitOK, out: "-kubeconfig"},
		{args: []string{"version"}, code: exitOK, out: "cogpit-cli " + buildinfo.Read().String()},
		{args: []string{"--version"}, code: exitOK, out: "cogpit-cli " + buildinfo.Read().String()},
		{args: []string{"version", "extra"}, code: exitUsage, errOut: "takes no arguments"},
		{args: []string{"frobnicate"}, code: exitUsage, errOut: `unknown command "frobnicate"`},
		{args: []string{"contexts", "--nope"}, code: exitUsage, errOut: "flag provided but not defined"},
		{args: []string{"contexts", "-o", "xml"}, code: exitUsage, errOut: `unknown output format "xml"`},
	}
	for _, c := range cases {
		t.Run(strings.Join(c.args, " "), func(t *testing.T) {
			te := newTestEnv(t)
			if code := te.run(t, c.args...); code != c.code {
				t.Errorf("exit code %d, want %d; stderr: %s", code, c.code, te.errOut)
			}
			if !strings.Contains(te.out.String(), c.out) {
				t.Errorf("stdout lacks %q:\n%s", c.out, te.out)
			}
			if !strings.Contains(te.errOut.String(), c.errOut) {
				t.Errorf("stderr lacks %q:\n%s", c.errOut, te.errOut)
			}
		})
	}
}

// TestNoConfigDirNoConnection: the app merges the kubeconfig files as kubectl does when it has no
// configuration directory, which runs their plugins unasked; the CLI refuses instead.
func TestNoConfigDirNoConnection(t *testing.T) {
	te := newTestEnv(t)
	te.dirs, te.dirsErr = app.Dirs{}, errors.New("neither $XDG_CONFIG_HOME nor $HOME are defined")
	if code := te.run(t, "contexts"); code != exitError {
		t.Fatalf("exit code %d, want %d", code, exitError)
	}
	if !strings.Contains(te.errOut.String(), "no credential plugin approvals") {
		t.Errorf("stderr: %s", te.errOut)
	}
}
