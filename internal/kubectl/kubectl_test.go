package kubectl

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestParseVersion(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want Version
		bad  bool
	}{
		{in: "v1.36.1", want: Version{1, 36, 1}},
		{in: "1.36.1", want: Version{1, 36, 1}},
		{in: " v1.30.4+k3s1 ", want: Version{1, 30, 4}},
		{in: "v1.29.3-gke.1093000", want: Version{1, 29, 3}},
		{in: "v1.28.9-eks-036c24b", want: Version{1, 28, 9}},
		{in: "v1.37.0-alpha.1", want: Version{1, 37, 0}},
		{in: "v1.36", bad: true},
		{in: "", bad: true},
		{in: "latest", bad: true},
		{in: "v1.36.1 extra", bad: true},
		{in: "v1.36.1\n; rm -rf /", bad: true},
		{in: "v1.9999999.1", bad: true}, // more than six digits
	} {
		got, err := ParseVersion(tc.in)
		if tc.bad {
			if err == nil {
				t.Errorf("ParseVersion(%q) = %v, want an error", tc.in, got)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("ParseVersion(%q) = %v, %v; want %v", tc.in, got, err, tc.want)
		}
	}
	v := Version{1, 36, 1}
	if v.String() != "1.36.1" || v.Tag() != "v1.36.1" {
		t.Errorf("String %q Tag %q", v.String(), v.Tag())
	}
}

// TestSupported is the skew rule: kubectl within one minor version of the API server, older or
// newer, and the same major version.
func TestSupported(t *testing.T) {
	server := Version{1, 36, 1}
	for _, tc := range []struct {
		client Version
		want   bool
	}{
		{Version{1, 36, 1}, true},
		{Version{1, 36, 0}, true},
		{Version{1, 36, 9}, true},
		{Version{1, 35, 3}, true},
		{Version{1, 37, 0}, true},
		{Version{1, 34, 2}, false},
		{Version{1, 38, 0}, false},
		{Version{1, 30, 2}, false},
		{Version{2, 36, 1}, false},
		{Version{0, 36, 1}, false},
	} {
		if got := Supported(tc.client, server); got != tc.want {
			t.Errorf("Supported(%s, %s) = %v, want %v", tc.client, server, got, tc.want)
		}
	}
	if Matching(Version{1, 36, 1}) != (Version{1, 36, 1}) {
		t.Error("the matching release of 1.36.1 is not 1.36.1")
	}
}

func writeExecutable(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

// TestInstalledAndBest: the releases Download left, newest first, and the one to run against a
// server: the closest minor, the newest among equals.
func TestInstalledAndBest(t *testing.T) {
	dir := t.TempDir()
	for _, v := range []string{"1.34.2", "1.35.0", "1.36.0", "1.36.3", "1.37.1"} {
		writeExecutable(t, filepath.Join(dir, v, "kubectl"), "#!/bin/sh\n", 0o755)
	}
	if runtime.GOOS != "windows" { // Windows keeps no execute bit: there every regular kubectl counts
		writeExecutable(t, filepath.Join(dir, "1.33.0", "kubectl"), "not executable", 0o600) // skipped
	}
	writeExecutable(t, filepath.Join(dir, "v1.36.9", "kubectl"), "#!/bin/sh\n", 0o755) // not Download's name
	writeExecutable(t, filepath.Join(dir, "notes", "kubectl"), "#!/bin/sh\n", 0o755)
	if err := os.MkdirAll(filepath.Join(dir, "1.32.0"), 0o700); err != nil { // no binary
		t.Fatal(err)
	}
	got := Installed(dir, "linux")
	var names []string
	for _, in := range got {
		names = append(names, in.Version.String())
		if in.Path != filepath.Join(dir, in.Version.String(), "kubectl") {
			t.Errorf("path %s", in.Path)
		}
	}
	if strings.Join(names, " ") != "1.37.1 1.36.3 1.36.0 1.35.0 1.34.2" {
		t.Fatalf("installed %v", names)
	}
	for _, tc := range []struct {
		server string
		want   string
	}{
		{"1.36.1", "1.36.3"}, // the same minor, newest patch
		{"1.35.7", "1.35.0"},
		{"1.38.0", "1.37.1"},
		{"1.33.4", "1.34.2"},
		{"1.31.0", ""},
	} {
		best, ok := Best(got, mustVersion(t, tc.server))
		if tc.want == "" {
			if ok {
				t.Errorf("server %s: best %s, want none", tc.server, best.Version)
			}
			continue
		}
		if !ok || best.Version.String() != tc.want {
			t.Errorf("server %s: best %s (%v), want %s", tc.server, best.Version, ok, tc.want)
		}
	}
	if Installed(filepath.Join(dir, "missing"), "linux") != nil {
		t.Error("a missing directory lists releases")
	}
}

func mustVersion(t *testing.T, s string) Version {
	t.Helper()
	v, err := ParseVersion(s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// TestProberCachesPerFile: a binary is run once while it is unchanged, again when it changed.
func TestProberCachesPerFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "kubectl")
	writeExecutable(t, path, "one", 0o755)
	var runs atomic.Int32
	answer := `{"clientVersion":{"major":"1","minor":"30","gitVersion":"v1.30.2"},"kustomizeVersion":"v5.4.2"}`
	p := NewProber()
	p.run = func(context.Context, string) ([]byte, error) { runs.Add(1); return []byte(answer), nil }
	for range 3 {
		v, err := p.ClientVersion(context.Background(), path)
		if err != nil || v != (Version{1, 30, 2}) {
			t.Fatalf("ClientVersion = %v, %v", v, err)
		}
	}
	if runs.Load() != 1 {
		t.Errorf("ran %d times for an unchanged file, want 1", runs.Load())
	}
	answer = `{"clientVersion":{"gitVersion":"v1.36.1"}}`
	writeExecutable(t, path, "two, longer", 0o755)
	if v, err := p.ClientVersion(context.Background(), path); err != nil || v != (Version{1, 36, 1}) || runs.Load() != 2 {
		t.Errorf("after a change: %v, %v, %d runs", v, err, runs.Load())
	}

	p.run = func(context.Context, string) ([]byte, error) { return []byte("kubectl: command not understood"), nil }
	other := filepath.Join(t.TempDir(), "kubectl")
	writeExecutable(t, other, "x", 0o755)
	if _, err := p.ClientVersion(context.Background(), other); err == nil {
		t.Error("output without JSON gave a version")
	}
	if _, err := p.ClientVersion(context.Background(), filepath.Join(t.TempDir(), "none")); err == nil {
		t.Error("a missing file gave a version")
	}
}

// TestProberRunsTheBinary runs a fake kubectl, a shell script, as "version --client -o json".
func TestProberRunsTheBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a shell script")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "kubectl")
	writeExecutable(t, path, fakeKubectl("v1.35.4"), 0o755)
	v, err := NewProber().ClientVersion(context.Background(), path)
	if err != nil || v != (Version{1, 35, 4}) {
		t.Fatalf("ClientVersion = %v, %v", v, err)
	}
	// It is asked for the client version only: other arguments fail.
	wrong := filepath.Join(dir, "wrong")
	writeExecutable(t, wrong, "#!/bin/sh\necho 'boom' >&2\nexit 3\n", 0o755)
	if _, err := NewProber().ClientVersion(context.Background(), wrong); err == nil {
		t.Error("a failing binary gave a version")
	}
	chatty := filepath.Join(dir, "chatty")
	writeExecutable(t, chatty, "#!/bin/sh\nhead -c 200000 /dev/zero\n", 0o755)
	if _, err := NewProber().ClientVersion(context.Background(), chatty); err == nil || !strings.Contains(err.Error(), "64.0 KiB of output") {
		t.Errorf("a binary that prints 200,000 bytes: %v", err)
	}
	// exec: the process the timeout kills is the one that holds the output open; a sleep the
	// shell forked would keep it open until the probe's WaitDelay, and outlive the test.
	slow := filepath.Join(dir, "slow")
	writeExecutable(t, slow, "#!/bin/sh\nexec sleep 30\n", 0o755)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := NewProber().ClientVersion(ctx, slow); err == nil {
		t.Error("a binary that does not answer gave a version")
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("waited %s for a binary that does not answer", d)
	}
}

// fakeKubectl is a shell script that answers "version --client -o json" as kubectl does.
func fakeKubectl(gitVersion string) string {
	return "#!/bin/sh\n" +
		`if [ "$1 $2 $3 $4" = "version --client -o json" ]; then` + "\n" +
		`  printf '{"clientVersion":{"major":"1","gitVersion":"` + gitVersion + `"}}\n'` + "\n" +
		"  exit 0\nfi\necho \"fake kubectl: $*\"\n"
}

func TestCheckMirror(t *testing.T) {
	for _, tc := range []struct {
		in, want string
	}{
		{"", DefaultMirror},
		{"  ", DefaultMirror},
		{"https://dl.k8s.io/release", "https://dl.k8s.io/release"},
		{"https://mirror.example.com/kubernetes/kubectl/", "https://mirror.example.com/kubernetes/kubectl"},
		{"https://mirror.example.com:8443/k8s", "https://mirror.example.com:8443/k8s"},
	} {
		if got, err := CheckMirror(tc.in); err != nil || got != tc.want {
			t.Errorf("CheckMirror(%q) = %q, %v; want %q", tc.in, got, err, tc.want)
		}
	}
	for _, bad := range []string{
		"http://dl.k8s.io/release",
		"dl.k8s.io/release",
		"https://user:secret@mirror.example.com/k8s",
		"https://mirror.example.com/k8s?token=1",
		"https://mirror.example.com/k8s#x",
		"https://mirror.example.com/k8s\nX-Injected: 1",
		"file:///etc/passwd",
		"https://" + strings.Repeat("a", 3000) + ".example.com",
	} {
		if got, err := CheckMirror(bad); err == nil {
			t.Errorf("CheckMirror(%q) = %q, want an error", bad, got)
		} else if strings.Contains(err.Error(), "secret") {
			t.Errorf("the error echoes the password: %v", err)
		}
	}
}
