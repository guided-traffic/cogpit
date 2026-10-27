package helm

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// unsetEnv clears an environment variable for the test (restored afterwards).
func unsetEnv(t *testing.T, name string) {
	t.Helper()
	t.Setenv(name, "")
	if err := os.Unsetenv(name); err != nil {
		t.Fatal(err)
	}
}

// The repositories file is the helm CLI's: $HELM_REPOSITORY_CONFIG, else $HELM_CONFIG_HOME's
// repositories.yaml, else Helm's configuration folder under the home directory (here a temporary
// one: no test resolves the developer's own file).
func TestRepositoryConfigPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming")) // a home on Windows (testing.md)
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
	t.Setenv("HELM_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("HELM_DATA_HOME", filepath.Join(home, "data"))
	unsetEnv(t, "XDG_CONFIG_HOME")

	t.Setenv("HELM_REPOSITORY_CONFIG", filepath.Join(home, "explicit.yaml"))
	t.Setenv("HELM_CONFIG_HOME", filepath.Join(home, "config"))
	if got := RepositoryConfigPath(); got != filepath.Join(home, "explicit.yaml") {
		t.Errorf("with HELM_REPOSITORY_CONFIG: %s", got)
	}
	unsetEnv(t, "HELM_REPOSITORY_CONFIG")
	if got := RepositoryConfigPath(); got != filepath.Join(home, "config", "repositories.yaml") {
		t.Errorf("with HELM_CONFIG_HOME: %s", got)
	}
	unsetEnv(t, "HELM_CONFIG_HOME")
	var want string
	switch runtime.GOOS {
	case "darwin":
		want = filepath.Join(home, "Library", "Preferences", "helm", "repositories.yaml")
	case "linux":
		want = filepath.Join(home, ".config", "helm", "repositories.yaml")
	case "windows":
		want = filepath.Join(home, "AppData", "Roaming", "helm", "repositories.yaml")
	default:
		t.Skip("Helm's default folder on " + runtime.GOOS + " is not under HOME")
	}
	if got := RepositoryConfigPath(); got != want {
		t.Errorf("Helm's default: %s, want %s", got, want)
	}
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "xdg"))
	if got := RepositoryConfigPath(); got != filepath.Join(home, "xdg", "helm", "repositories.yaml") {
		t.Errorf("with XDG_CONFIG_HOME: %s", got)
	}
}

// writeFile writes a test file.
func writeFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

// Helm's file is parsed with the SDK's types; each entry becomes a Repository Cogpit downloads
// from, or is listed with why it is not used. Credentials stay in the Repository and print nowhere.
func TestReadRepositoryFile(t *testing.T) {
	dir := t.TempDir()
	ca, cert, key := filepath.Join(dir, "ca.pem"), filepath.Join(dir, "client.pem"), filepath.Join(dir, "client-key.pem")
	path := filepath.Join(dir, "repositories.yaml")
	writeFile(t, path, fmt.Sprintf(`apiVersion: ""
generated: "2026-10-13T09:00:00Z"
repositories:
- name: stable
  url: https://charts.example.com/stable/
- name: private
  url: https://charts.example.com/private
  username: alice
  password: pw-in-the-file-41
  caFile: %s
  insecure_skip_tls_verify: true
  pass_credentials_all: true
- name: certs
  url: https://certs.example.com
  certFile: %s
  keyFile: %s
- name: plain
  url: http://plain.example.com/charts
- name: inurl
  url: https://bob:pw-in-the-url-42@inurl.example.com/charts?token=tok-43
- name: with.dot
  url: https://dot.example.com
- name: oci
  url: oci://registry.example.com/charts
- name: stable
  url: https://another.example.com
- name: relative
  url: https://rel.example.com
  caFile: ca.pem
- null
- name: "with:colon"
  url: https://colon.example.com
- name: .dot-first
  url: https://dotfirst.example.com
`, ca, cert, key))
	entries, err := ReadRepositoryFile(path)
	if err != nil {
		t.Fatal(err)
	}
	byName := func(i int) RepositoryEntry { return entries[i] }
	if len(entries) != 11 {
		t.Fatalf("%d entries: %+v", len(entries), entries)
	}
	// A name with a dot is used since A36 (2026-10-14): the chart id doubles the dot.
	if d := byName(5); d.Name != "with.dot" || d.Repository == nil || d.Err != nil || d.Repository.Name != "with.dot" {
		t.Errorf("with.dot %+v", d)
	}
	if e := byName(0); e.Repository == nil || e.Repository.URL != "https://charts.example.com/stable" || e.HasCredentials || e.PlainHTTP {
		t.Errorf("stable %+v", e)
	}
	p := byName(1)
	if p.Repository == nil || !p.HasCredentials || !p.CAFile || !p.InsecureSkipTLSVerify || !p.PassCredentialsAll ||
		!p.Repository.HasBasicAuth() || p.Repository.CAFile != ca || p.ClientCertificate {
		t.Errorf("private %+v", p)
	}
	if c := byName(2); c.Repository == nil || !c.ClientCertificate || !c.HasCredentials || c.Repository.HasBasicAuth() {
		t.Errorf("certs %+v", c)
	}
	if h := byName(3); h.Repository == nil || !h.PlainHTTP || !h.Repository.PlainHTTP() {
		t.Errorf("plain %+v", h)
	}
	in := byName(4)
	if in.Repository == nil || !in.HasCredentials || !in.Repository.HasBasicAuth() ||
		in.Repository.URL != "https://inurl.example.com/charts?token=tok-43" || in.URL != "https://inurl.example.com/charts?token=•••" {
		t.Errorf("a user in the URL: %+v (%s)", in, in.Repository.URL)
	}
	for i, want := range map[int]string{6: "OCI", 7: "a second entry", 8: "no absolute path", 9: "letters, digits, '.', '-' or '_'", 10: "the first a letter or a digit"} {
		if e := byName(i); e.Repository != nil || e.Err == nil || !strings.Contains(e.Err.Error(), want) {
			t.Errorf("entry %d %s: %+v, want %q", i, e.Name, e, want)
		}
	}

	// Nothing that prints a Repository shows its credentials.
	var logged bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logged, nil))
	for _, e := range entries {
		if e.Repository == nil {
			continue
		}
		log.Info("repository", "repo", e.Repository, "value", *e.Repository)
		js, _ := json.Marshal(e.Repository)
		for _, text := range []string{fmt.Sprint(e.Repository), fmt.Sprintf("%+v", *e.Repository), fmt.Sprintf("%#v", *e.Repository),
			fmt.Sprintf("%v", e), string(js), e.URL, logged.String()} {
			for _, secret := range []string{"pw-in-the-file-41", "pw-in-the-url-42", "tok-43"} {
				if strings.Contains(text, secret) {
					t.Errorf("%s shows %s: %s", e.Name, secret, text)
				}
			}
		}
	}
	// The fingerprint follows the credentials; the cache key does not.
	other := *p.Repository
	changed := other.WithBasicAuth("alice", "another")
	if changed.Fingerprint() == p.Repository.Fingerprint() || changed.CacheKey() != p.Repository.CacheKey() {
		t.Error("a changed password: fingerprint or cache key wrong")
	}
	moved := *p.Repository
	moved.URL = "https://charts.example.com/moved"
	if moved.CacheKey() == p.Repository.CacheKey() {
		t.Error("a changed URL keeps the cache key")
	}
}

// A missing file is fs.ErrNotExist; one that does not parse names its line, never its content; a
// file over the bound or a folder is refused.
func TestReadRepositoryFileRefusals(t *testing.T) {
	dir := t.TempDir()
	if _, err := ReadRepositoryFile(filepath.Join(dir, "none.yaml")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("a missing file: %v", err)
	}
	broken := filepath.Join(dir, "broken.yaml")
	writeFile(t, broken, "repositories:\n- name: x\n  password: pw-broken-44\n  url: [unclosed\n")
	_, err := ReadRepositoryFile(broken)
	if err == nil || !strings.Contains(err.Error(), "does not parse") || strings.Contains(err.Error(), "pw-broken-44") || strings.Contains(err.Error(), "unclosed") {
		t.Errorf("a broken file: %v", err)
	}
	wrong := filepath.Join(dir, "wrong.yaml")
	writeFile(t, wrong, "repositories: pw-wrong-45\n")
	if _, err := ReadRepositoryFile(wrong); err == nil || strings.Contains(err.Error(), "pw-wrong-45") {
		t.Errorf("a file of another shape: %v", err)
	}
	big := filepath.Join(dir, "big.yaml")
	writeFile(t, big, "repositories: []\n#"+strings.Repeat("x", int(MaxRepositoryFileBytes)))
	if _, err := ReadRepositoryFile(big); err == nil || !strings.Contains(err.Error(), "larger than the 1.0 MiB Cogpit reads") {
		t.Errorf("a file over the bound: %v", err)
	}
	if _, err := ReadRepositoryFile(dir); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Errorf("a folder: %v", err)
	}
	empty := filepath.Join(dir, "empty.yaml")
	writeFile(t, empty, "")
	if entries, err := ReadRepositoryFile(empty); err != nil || len(entries) != 0 {
		t.Errorf("an empty file: %v %v", entries, err)
	}
}

// RedactURL drops a user and password and hides a query's values.
func TestRedactURL(t *testing.T) {
	for in, want := range map[string]string{
		"https://charts.example.com/x":           "https://charts.example.com/x",
		"https://u:p@charts.example.com/x":       "https://charts.example.com/x",
		"https://charts.example.com/x?token=abc": "https://charts.example.com/x?token=•••",
		"http://u@plain.example.com/index.yaml":  "http://plain.example.com/index.yaml",
		"https://charts.example.com/%zz":         "(a URL that does not parse)",
		"https://charts.example.com/x#frag":      "https://charts.example.com/x#frag",
	} {
		if got := RedactURL(in); got != want {
			t.Errorf("%s: %s, want %s", in, got, want)
		}
	}
}
