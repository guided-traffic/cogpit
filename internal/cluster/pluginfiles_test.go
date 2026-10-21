package cluster

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/guided-traffic/cogpit/internal/kubeconfig"
)

// loginKubeconfig is a kubeconfig whose only context logs in with the given user fields (YAML,
// indented by six spaces) against a cluster with the given extra cluster fields (indented by four).
func loginKubeconfig(userFields, clusterFields string) string {
	return `apiVersion: v1
kind: Config
clusters:
- name: c
  cluster:
    server: https://login.example:6443
` + clusterFields + `users:
- name: u
  user:
` + userFields + `contexts:
- name: x
  context: {cluster: c, user: u}
`
}

func execFields(command string) string {
	return "    exec:\n      apiVersion: client.authentication.k8s.io/v1\n      interactiveMode: Never\n      command: " + command + "\n"
}

func putFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

// inspectLogin writes the kubeconfig into dir and returns the login of its context x.
func inspectLogin(t *testing.T, dir, text string) *kubeconfig.ExecPlugin {
	t.Helper()
	file := filepath.Join(dir, "kc.yaml")
	putFile(t, file, text, 0o600)
	in, err := InspectFile(file)
	if err != nil {
		t.Fatal(err)
	}
	return in.Exec["x"]
}

func TestProgramInsideTheFolderIsBoundByContent(t *testing.T) {
	dir := t.TempDir()
	putFile(t, filepath.Join(dir, "bin", "plug"), "#!/bin/sh\necho 1\n", 0o755)
	// client-go resolves a command against the kubeconfig's folder only when it holds the system's
	// separator: .\bin\plug on Windows, where ./bin/plug is looked up from the working directory.
	text := loginKubeconfig(execFields(filepath.FromSlash("./bin/plug")), "")
	a := inspectLogin(t, dir, text)
	if a.Files.ProgramSHA256 == "" || a.Files.ProgramNote != "" {
		t.Fatalf("a program inside the folder is hashed: %+v", a.Files)
	}
	again := inspectLogin(t, dir, text)
	if again.Fingerprint() != a.Fingerprint() {
		t.Error("the fingerprint of an unchanged program is stable")
	}
	putFile(t, filepath.Join(dir, "bin", "plug"), "#!/bin/sh\necho 2\n", 0o755)
	if changed := inspectLogin(t, dir, text); changed.Fingerprint() == a.Fingerprint() {
		t.Error("replacing the program must change the fingerprint")
	}
}

// A program named without an extension is bound by the file that runs: on Windows os/exec adds the
// first of PATHEXT's extensions that exists, so plug runs plug.exe and a plug beside it binds
// nothing; elsewhere plug itself runs.
func TestProgramIsBoundByTheFileThatRuns(t *testing.T) {
	dir := t.TempDir()
	runs, beside := filepath.Join(dir, "bin", "plug"), filepath.Join(dir, "bin", "plug.exe")
	if runtime.GOOS == "windows" {
		runs, beside = beside, runs
	}
	putFile(t, runs, "runs", 0o755)
	putFile(t, beside, "beside", 0o755)
	text := loginKubeconfig(execFields(filepath.FromSlash("./bin/plug")), "")
	a := inspectLogin(t, dir, text)
	if sum := sha256.Sum256([]byte("runs")); a.Files.ProgramSHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("the file that runs is hashed: %+v", a.Files)
	}
	putFile(t, beside, "beside, changed", 0o755)
	if b := inspectLogin(t, dir, text); b.Fingerprint() != a.Fingerprint() {
		t.Error("a file that does not run changed the fingerprint")
	}
	putFile(t, runs, "runs, changed", 0o755)
	if c := inspectLogin(t, dir, text); c.Fingerprint() == a.Fingerprint() {
		t.Error("replacing the file that runs must change the fingerprint")
	}
}

func TestProgramOutsideTheFolderIsBoundByPathOnly(t *testing.T) {
	dir, tools := t.TempDir(), t.TempDir()
	tool := filepath.Join(tools, "aws")
	putFile(t, tool, "#!/bin/sh\necho 1\n", 0o755)
	a := inspectLogin(t, dir, loginKubeconfig(execFields(tool), ""))
	if a.Files.ProgramSHA256 != "" || a.Files.ProgramResolved != "" {
		t.Fatalf("a program outside the folder is bound by its path alone: %+v", a.Files)
	}
	putFile(t, tool, "#!/bin/sh\necho a tool update\n", 0o755)
	if b := inspectLogin(t, dir, loginKubeconfig(execFields(tool), "")); b.Fingerprint() != a.Fingerprint() {
		t.Error("a tool update outside the folder must not ask again")
	}
}

func TestProgramFoundOnPathIsBoundByPathOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX PATH lookup")
	}
	dir, tools := t.TempDir(), t.TempDir()
	putFile(t, filepath.Join(tools, "kubelogin-test"), "#!/bin/sh\necho 1\n", 0o755)
	t.Setenv("PATH", tools+string(os.PathListSeparator)+os.Getenv("PATH"))
	a := inspectLogin(t, dir, loginKubeconfig(execFields("kubelogin-test"), ""))
	if a.Files.ProgramSHA256 != "" {
		t.Fatalf("a PATH program is not hashed: %+v", a.Files)
	}
	putFile(t, filepath.Join(tools, "kubelogin-test"), "#!/bin/sh\necho 2\n", 0o755)
	if b := inspectLogin(t, dir, loginKubeconfig(execFields("kubelogin-test"), "")); b.Fingerprint() != a.Fingerprint() {
		t.Error("an update of a PATH tool must not ask again")
	}
}

func TestSymlinkInTheFolderIsBoundByItsTarget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks")
	}
	dir, tools := t.TempDir(), t.TempDir()
	one, two := filepath.Join(tools, "one"), filepath.Join(tools, "two")
	putFile(t, one, "1", 0o755)
	putFile(t, two, "2", 0o755)
	link := filepath.Join(dir, "plug")
	if err := os.Symlink(one, link); err != nil {
		t.Fatal(err)
	}
	text := loginKubeconfig(execFields("./plug"), "")
	a := inspectLogin(t, dir, text)
	if a.Files.ProgramSHA256 != "" || a.Files.ProgramResolved == "" {
		t.Fatalf("a link in the folder to a file outside: target bound, content not: %+v", a.Files)
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(two, link); err != nil {
		t.Fatal(err)
	}
	if b := inspectLogin(t, dir, text); b.Fingerprint() == a.Fingerprint() {
		t.Error("repointing a link in the folder must ask again")
	}

	// A link outside the folder to a file inside it: the content of the target is bound.
	inner := filepath.Join(dir, "real")
	putFile(t, inner, "x", 0o755)
	out := filepath.Join(tools, "alias")
	if err := os.Symlink(inner, out); err != nil {
		t.Fatal(err)
	}
	c := inspectLogin(t, dir, loginKubeconfig(execFields(out), ""))
	if c.Files.ProgramSHA256 == "" {
		t.Errorf("a program that resolves into the folder is hashed: %+v", c.Files)
	}
}

func TestProgramThatCannotBeHashedIsBoundByANote(t *testing.T) {
	dir := t.TempDir()
	plug := filepath.Join(dir, "plug")
	f, err := os.Create(plug)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(MaxBoundProgramSize + 1); err != nil { // sparse
		t.Fatal(err)
	}
	_ = f.Close()
	if err := os.Chmod(plug, 0o755); err != nil {
		t.Fatal(err)
	}
	text := loginKubeconfig(execFields(filepath.FromSlash("./plug")), "")
	a := inspectLogin(t, dir, text)
	if a.Files.ProgramSHA256 != "" || !strings.Contains(a.Files.ProgramNote, "larger than") {
		t.Fatalf("too large: %+v", a.Files)
	}
	if err := os.Mkdir(filepath.Join(dir, "d"), 0o755); err != nil {
		t.Fatal(err)
	}
	d := inspectLogin(t, dir, loginKubeconfig(execFields(filepath.FromSlash("./d")), ""))
	if d.Files.ProgramNote != "not a regular file" {
		t.Errorf("a directory: %+v", d.Files)
	}
}

func TestCAFileInsideTheFolderIsBoundByContent(t *testing.T) {
	dir, certs := t.TempDir(), t.TempDir()
	putFile(t, filepath.Join(dir, "ca.pem"), "one", 0o644)
	putFile(t, filepath.Join(certs, "ca.pem"), "one", 0o644)
	tool := filepath.Join(certs, "aws")
	putFile(t, tool, "x", 0o755)

	inside := loginKubeconfig(execFields(tool), "    certificate-authority: ca.pem\n")
	a := inspectLogin(t, dir, inside)
	if a.Files.CAFileSHA256 == "" {
		t.Fatalf("a CA file inside the folder is hashed: %+v", a.Files)
	}
	putFile(t, filepath.Join(dir, "ca.pem"), "two", 0o644)
	if b := inspectLogin(t, dir, inside); b.Fingerprint() == a.Fingerprint() {
		t.Error("replacing the CA file must ask again")
	}

	outside := loginKubeconfig(execFields(tool), "    certificate-authority: "+filepath.Join(certs, "ca.pem")+"\n")
	c := inspectLogin(t, dir, outside)
	putFile(t, filepath.Join(certs, "ca.pem"), "rotated", 0o644)
	if d := inspectLogin(t, dir, outside); d.Fingerprint() != c.Fingerprint() || c.Files.CAFileSHA256 != "" {
		t.Error("a CA file outside the folder is bound by path alone")
	}
}

func TestTokenFileOutsideTheFolderNeedsApproval(t *testing.T) {
	dir, elsewhere := t.TempDir(), t.TempDir()
	putFile(t, filepath.Join(dir, "token"), "tok-in-folder", 0o600)
	putFile(t, filepath.Join(elsewhere, "token"), "tok-elsewhere", 0o600)

	if p := inspectLogin(t, dir, loginKubeconfig("    tokenFile: token\n", "")); p != nil {
		t.Errorf("a token file in the kubeconfig's folder needs no approval: %+v", p)
	}
	if p := inspectLogin(t, dir, loginKubeconfig("    token: inline\n", "")); p != nil {
		t.Errorf("an inline token needs no approval: %+v", p)
	}
	out := filepath.Join(elsewhere, "token")
	a := inspectLogin(t, dir, loginKubeconfig("    tokenFile: "+out+"\n", ""))
	if a == nil || !a.AsksForTokenFile() || a.Command != "" || a.TokenFile != out || a.Server != "https://login.example:6443" {
		t.Fatalf("a token file outside the folder is asked for: %+v", a)
	}
	// Bound to the path and the server.
	if b := inspectLogin(t, dir, loginKubeconfig("    tokenFile: "+filepath.Join(elsewhere, "other")+"\n", "")); b.Fingerprint() == a.Fingerprint() {
		t.Error("a changed path must ask again")
	}
	other := strings.Replace(loginKubeconfig("    tokenFile: "+out+"\n", ""), "login.example", "evil.example", 1)
	if c := inspectLogin(t, dir, other); c.Fingerprint() == a.Fingerprint() {
		t.Error("a changed server must ask again")
	}
	// The content of the file is not bound: tokens rotate.
	putFile(t, out, "tok-rotated", 0o600)
	if d := inspectLogin(t, dir, loginKubeconfig("    tokenFile: "+out+"\n", "")); d.Fingerprint() != a.Fingerprint() {
		t.Error("a rotated token must not ask again")
	}
}

func TestTokenFileLinkedOutOfTheFolderNeedsApproval(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks")
	}
	dir, elsewhere := t.TempDir(), t.TempDir()
	secret := filepath.Join(elsewhere, "secret")
	putFile(t, secret, "tok", 0o600)
	if err := os.Symlink(secret, filepath.Join(dir, "token")); err != nil {
		t.Fatal(err)
	}
	p := inspectLogin(t, dir, loginKubeconfig("    tokenFile: token\n", ""))
	if p == nil || !p.AsksForTokenFile() || p.Files.TokenFileResolved == "" {
		t.Fatalf("a link in the folder that points out is outside: %+v", p)
	}
}
