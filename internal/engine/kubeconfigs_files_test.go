package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/kubeconfig"
)

// sourcesWith adds file as a kubeconfig source of a fresh store and returns it.
func sourcesWith(t *testing.T, file string) *KubeconfigSources {
	t.Helper()
	k := NewKubeconfigSources(filepath.Join(t.TempDir(), "Cogpit"))
	if _, _, err := k.AddFile(file); err != nil {
		t.Fatal(err)
	}
	return k
}

// connectError connects to the context kctx of file with the sources' policy and returns the
// error; the servers of these tests do not exist, so only a policy refusal is an approval error.
func connectError(k *KubeconfigSources, file, kctx string) error {
	_, err := cluster.Connect(context.Background(), cluster.Options{
		KubeconfigPaths: []string{file}, Context: kctx, ExecPolicy: k.Policy, ConnectTimeout: 200 * time.Millisecond,
	})
	return err
}

// TestExecApprovalRefusedAfterPluginBinaryChanged (review R-5): a plugin inside the kubeconfig's
// folder is bound by its content; replacing it asks again, at listing and at connect.
func TestExecApprovalRefusedAfterPluginBinaryChanged(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a shell script is the plugin")
	}
	dir := t.TempDir()
	file := filepath.Join(dir, "team.yaml")
	writeUserFile(t, file, execSourceKubeconfig("e", "./plug"))
	plug := filepath.Join(dir, "plug")
	if err := os.WriteFile(plug, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	k := sourcesWith(t, file)
	id := "e [team.yaml]"
	f, kctx, ok, err := k.Find(id)
	if err != nil || !ok {
		t.Fatalf("Find: %v %v", ok, err)
	}
	if err := k.Approve(f.Exec[kctx]); err != nil {
		t.Fatal(err)
	}
	if st, _ := k.ExecState(f.Exec[kctx]); st != ExecApproved {
		t.Fatalf("after approval: %s", st)
	}
	if err := connectError(k, file, kctx); errors.Is(err, ErrExecNotApproved) {
		t.Fatalf("an approved, unchanged plugin was refused: %v", err)
	}

	// The writer of the folder replaces the program (same size, same name).
	if err := os.WriteFile(plug, []byte("#!/bin/sh\nexit 2\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	f, kctx, _, _ = k.Find(id)
	if st, _ := k.ExecState(f.Exec[kctx]); st != ExecChanged {
		t.Errorf("after the program was replaced the listing says %q, want %q", st, ExecChanged)
	}
	if err := connectError(k, file, kctx); !errors.Is(err, ErrExecNotApproved) {
		t.Errorf("connecting after the program was replaced: %v, want a refusal (ErrExecNotApproved)", err)
	}
}

// tokenFileKubeconfig is a kubeconfig whose user reads its token from tokenFile, single-quoted: a
// Windows path's backslashes are no escapes there.
func tokenFileKubeconfig(server, tokenFile string) string {
	return `apiVersion: v1
kind: Config
clusters:
- name: c
  cluster: {server: "` + server + `"}
users:
- name: u
  user: {tokenFile: '` + tokenFile + `'}
contexts:
- name: t
  context: {cluster: c, user: u}
`
}

// TestTokenFileOutsideTheFolderIsAskedFor (D-17): connecting is refused until the user allowed the
// file, an allowance is bound to the path and the server, and a token file in the folder needs none.
func TestTokenFileOutsideTheFolderIsAskedFor(t *testing.T) {
	dir, elsewhere := t.TempDir(), t.TempDir()
	const secret = "tok-OUTSIDE-must-not-leak"
	token := filepath.Join(elsewhere, "token")
	writeUserFile(t, token, secret)
	file := filepath.Join(dir, "team.yaml")
	writeUserFile(t, file, tokenFileKubeconfig("https://t.example:6443", token))
	k := sourcesWith(t, file)
	id := "t [team.yaml]"

	err := connectError(k, file, "t")
	var ae *ExecApprovalError
	if !errors.Is(err, ErrExecNotApproved) || !errors.As(err, &ae) || ae.State != ExecRequired || ae.Plugin.Kind() != kubeconfig.LoginTokenFile {
		t.Fatalf("an unapproved token file outside the folder: %v", err)
	}
	if msg := err.Error(); !strings.Contains(msg, token) || !strings.Contains(msg, "t.example") || strings.Contains(msg, secret) {
		t.Errorf("the refusal names the file and the server and never the content: %q", msg)
	}
	ctxs, _ := k.Contexts()
	if len(ctxs) != 1 || ctxs[0].Exec != ExecRequired || ctxs[0].ExecKind != kubeconfig.LoginTokenFile {
		t.Errorf("the listing marks the context: %+v", ctxs)
	}

	f, kctx, ok, ferr := k.Find(id)
	if ferr != nil || !ok || f.Exec[kctx] == nil {
		t.Fatalf("Find: %v %v", ok, ferr)
	}
	if err := k.Approve(f.Exec[kctx]); err != nil {
		t.Fatal(err)
	}
	if err := connectError(k, file, "t"); errors.Is(err, ErrExecNotApproved) {
		t.Fatalf("an approved token file was refused: %v", err)
	}
	writeUserFile(t, token, "tok-rotated") // a rotated token is not a change
	if err := connectError(k, file, "t"); errors.Is(err, ErrExecNotApproved) {
		t.Errorf("a rotated token asked again: %v", err)
	}

	writeUserFile(t, file, tokenFileKubeconfig("https://t.example:6443", filepath.Join(elsewhere, "other")))
	if err := connectError(k, file, "t"); !errors.As(err, &ae) || ae.State != ExecChanged || !strings.Contains(err.Error(), "changed since you allowed it") {
		t.Errorf("a changed path asks again: %v", err)
	}
	writeUserFile(t, file, tokenFileKubeconfig("https://evil.example:6443", token))
	if err := connectError(k, file, "t"); !errors.As(err, &ae) || ae.State != ExecChanged {
		t.Errorf("a changed server asks again: %v", err)
	}

	// The same file inside the kubeconfig's folder comes from the same source: no approval.
	writeUserFile(t, filepath.Join(dir, "token"), "tok-in-folder")
	writeUserFile(t, file, tokenFileKubeconfig("https://t.example:6443", "token"))
	if err := connectError(k, file, "t"); errors.Is(err, ErrExecNotApproved) {
		t.Errorf("a token file inside the folder needs no approval: %v", err)
	}
}

// TestCAFileSwapAsksAgain (review R-5, certificate-authority): a CA file in the folder is bound by
// its content.
func TestCAFileSwapAsksAgain(t *testing.T) {
	dir := t.TempDir()
	writeUserFile(t, filepath.Join(dir, "ca.pem"), "one")
	file := filepath.Join(dir, "team.yaml")
	text := strings.Replace(execSourceKubeconfig("e", "aws"), `{server: "https://exec.example:6443"}`,
		`{server: "https://exec.example:6443", certificate-authority: ca.pem}`, 1)
	writeUserFile(t, file, text)
	k := sourcesWith(t, file)
	f, kctx, _, _ := k.Find("e [team.yaml]")
	if err := k.Approve(f.Exec[kctx]); err != nil {
		t.Fatal(err)
	}
	if st, _ := k.ExecState(f.Exec[kctx]); st != ExecApproved {
		t.Fatalf("after approval: %s", st)
	}
	writeUserFile(t, filepath.Join(dir, "ca.pem"), "two")
	f, kctx, _, _ = k.Find("e [team.yaml]")
	if st, _ := k.ExecState(f.Exec[kctx]); st != ExecChanged {
		t.Errorf("after the CA file was replaced: %s", st)
	}
}
