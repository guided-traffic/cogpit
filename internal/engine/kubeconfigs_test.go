package engine

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/kubeconfig"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// sourceSecret is a credential in the test kubeconfigs: no error, log or listing may contain it.
const sourceSecret = "tok-PASTED-6c1e-must-not-leak"

// sourceKubeconfig is a kubeconfig with one context and a token user.
func sourceKubeconfig(contextName string) string {
	return `apiVersion: v1
kind: Config
current-context: ` + contextName + `
clusters:
- name: c
  cluster:
    server: https://` + contextName + `.example:6443
users:
- name: u
  user:
    token: ` + sourceSecret + `
contexts:
- name: ` + contextName + `
  context:
    cluster: c
    user: u
    namespace: team
`
}

// execSourceKubeconfig is a kubeconfig whose user runs the plugin command with args.
func execSourceKubeconfig(contextName, command string, args ...string) string {
	return `apiVersion: v1
kind: Config
clusters:
- name: c
  cluster: {server: "https://exec.example:6443"}
users:
- name: u
  user:
    exec:
      apiVersion: client.authentication.k8s.io/v1
      interactiveMode: Never
      command: ` + command + `
      args: ["` + strings.Join(args, `", "`) + `"]
      env:
      - {name: SECRET_ENV, value: ` + sourceSecret + `}
contexts:
- name: ` + contextName + `
  context: {cluster: c, user: u}
`
}

func writeUserFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// fileState is what a file looks like: content, mode and modification time.
type fileState struct {
	data []byte
	mode fs.FileMode
	mod  time.Time
}

// treeState records every file under dir.
func treeState(t *testing.T, dir string) map[string]fileState {
	t.Helper()
	out := map[string]fileState{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		fi, err := os.Lstat(p)
		if err != nil {
			return err
		}
		st := fileState{mode: fi.Mode(), mod: fi.ModTime()}
		if fi.Mode().IsRegular() {
			if st.data, err = os.ReadFile(p); err != nil {
				return err
			}
		}
		out[p] = st
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func sameTree(t *testing.T, before, after map[string]fileState) {
	t.Helper()
	for p, b := range before {
		a, ok := after[p]
		switch {
		case !ok:
			t.Errorf("%s was removed", p)
		case !bytes.Equal(a.data, b.data):
			t.Errorf("%s changed content", p)
		case a.mode != b.mode || !a.mod.Equal(b.mod):
			t.Errorf("%s changed mode or time: %v %v → %v %v", p, b.mode, b.mod, a.mode, a.mod)
		}
	}
	for p := range after {
		if _, ok := before[p]; !ok {
			t.Errorf("%s was created in the user's files", p)
		}
	}
}

func checkMode(t *testing.T, path string, want fs.FileMode) {
	t.Helper()
	if runtime.GOOS == "windows" {
		return // no modes there: the profile's inherited ACLs protect Cogpit's files (privatefile.go)
	}
	fi, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := fi.Mode().Perm(); got != want {
		t.Errorf("%s: mode %#o, want %#o", path, got, want)
	}
}

// TestSourcesFileIsOwnerOnly: the list of sources is 0600 in a 0700 configuration directory (one
// created too open is tightened), holds no credential, and the added file is only read.
func TestSourcesFileIsOwnerOnly(t *testing.T) {
	cfgDir := filepath.Join(t.TempDir(), "Cogpit")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil { // as an older or careless writer left it
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "dev.yaml")
	writeUserFile(t, file, sourceKubeconfig("dev-ctx"))
	k := NewKubeconfigSources(cfgDir)
	src, ctxs, err := k.AddFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if src.ID != "dev.yaml" || src.Kind != SourceFile || len(ctxs) != 1 || ctxs[0].Name != "dev-ctx" {
		t.Fatalf("AddFile = %+v %+v", src, ctxs)
	}
	checkMode(t, cfgDir, 0o700)
	checkMode(t, filepath.Join(cfgDir, KubeconfigSourcesFile), 0o600)
	sources, err := os.ReadFile(filepath.Join(cfgDir, KubeconfigSourcesFile))
	if err != nil || strings.Contains(string(sources), sourceSecret) {
		t.Fatalf("the sources file: %v (or it holds the token)", err)
	}
	infos, err := k.Contexts()
	if err != nil {
		t.Fatal(err)
	}
	if len(infos) != 1 || infos[0].Name != "dev-ctx [dev.yaml]" || infos[0].KubeContext == "" || infos[0].File != file || infos[0].Exec != "" {
		t.Errorf("contexts %+v", infos)
	}
}

// TestPastedSourcesOfEarlierBuildsAreNotOffered: pasting was removed (D-21). A "pasted" entry that
// an earlier build wrote is not listed and offers no context, its file stays on disk untouched,
// the other sources keep working, and the next save of the list drops the entry.
func TestPastedSourcesOfEarlierBuildsAreNotOffered(t *testing.T) {
	cfgDir := filepath.Join(t.TempDir(), "Cogpit")
	pastedDir := filepath.Join(cfgDir, "kubeconfigs")
	pasted := filepath.Join(pastedDir, "pasted-1f2e3d4c5b6a7988.yaml")
	writeUserFile(t, pasted, sourceKubeconfig("old-ctx"))
	file := filepath.Join(t.TempDir(), "dev.yaml")
	writeUserFile(t, file, sourceKubeconfig("dev-ctx"))
	doc := `{"version":1,"sources":[` +
		`{"id":"pasted 1","kind":"pasted","path":` + strconv.Quote(pasted) + `,"added":"2026-10-11T10:00:00Z"},` +
		`{"id":"dev.yaml","kind":"file","path":` + strconv.Quote(file) + `,"added":"2026-10-11T10:01:00Z"}]}`
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, KubeconfigSourcesFile), []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	k := NewKubeconfigSources(cfgDir)
	list, err := k.List()
	if err != nil || len(list) != 1 || list[0].ID != "dev.yaml" {
		t.Fatalf("List = %+v, %v", list, err)
	}
	infos, err := k.Contexts()
	if err != nil || len(infos) != 1 || infos[0].Name != "dev-ctx [dev.yaml]" {
		t.Fatalf("Contexts = %+v, %v", infos, err)
	}
	if _, err := k.Remove("pasted 1"); err == nil {
		t.Error("Remove found a pasted source")
	}
	before := treeState(t, pastedDir)
	if _, err := k.AddFolder(t.TempDir()); err != nil {
		t.Fatal(err) // the save drops the old entry
	}
	if list, err := k.List(); err != nil || len(list) != 2 || slices.ContainsFunc(list, func(s KubeconfigSource) bool { return s.ID == "pasted 1" }) {
		t.Errorf("List after a save = %+v, %v", list, err)
	}
	sameTree(t, before, treeState(t, pastedDir))
	if got, err := os.ReadFile(pasted); err != nil || string(got) != sourceKubeconfig("old-ctx") {
		t.Errorf("the file of an old pasted source changed: %v", err)
	}
}

// TestAddedFilesAreNeverWritten adds a kubeconfig file and a sync folder, lists, resolves and
// approves their contexts, watches them and removes them: the user's files stay byte-identical,
// with the same modes and times, and nothing is created next to them.
func TestAddedFilesAreNeverWritten(t *testing.T) {
	user := t.TempDir()
	file := filepath.Join(user, "kube", "dev.yaml")
	writeUserFile(t, file, sourceKubeconfig("dev"))
	folder := filepath.Join(user, "synced")
	writeUserFile(t, filepath.Join(folder, "team.yaml"), execSourceKubeconfig("team", "aws", "eks", "get-token"))
	writeUserFile(t, filepath.Join(folder, "notes.txt"), "not a kubeconfig\n")
	before := treeState(t, user)

	k := NewKubeconfigSources(filepath.Join(t.TempDir(), "Cogpit"))
	if _, _, err := k.AddFile(file); err != nil {
		t.Fatal(err)
	}
	if _, err := k.AddFolder(folder); err != nil {
		t.Fatal(err)
	}
	s := New(Options{Connector: &ClusterConnector{KubeconfigPaths: []string{filepath.Join(user, "absent")}, Sources: k}})
	defer s.Close()
	stop := s.WatchKubeconfigs(time.Millisecond)
	infos, _ := s.Contexts()
	var ids []string
	for _, c := range infos {
		ids = append(ids, c.Name)
	}
	if strings.Join(ids, ",") != "dev [dev.yaml],team [synced/team.yaml]" {
		t.Fatalf("contexts %v", ids)
	}
	info, err := s.ExecPlugin("team [synced/team.yaml]")
	if err != nil || info.State != ExecRequired {
		t.Fatalf("ExecPlugin: %+v %v", info, err)
	}
	if _, err := s.ApproveExecAll([]ExecApprovalItem{{Context: info.Context, Fingerprint: info.Fingerprint}}); err != nil {
		t.Fatal(err)
	}
	_, statuses, err := k.Files()
	if err != nil || len(statuses) != 2 || statuses[1].Skipped != 1 || statuses[1].Contexts != 1 || statuses[0].Contexts != 1 {
		t.Fatalf("statuses %+v %v", statuses, err)
	}
	time.Sleep(20 * time.Millisecond) // let the watcher poll a few times: polling must not write either
	stop()
	for _, id := range []string{"dev.yaml", "synced/"} {
		if _, err := k.Remove(id); err != nil {
			t.Fatal(err)
		}
	}
	sameTree(t, before, treeState(t, user))
	if a, err := k.approvals(); err != nil || len(a) != 0 {
		t.Errorf("removing the folder kept its approvals: %+v %v", a, err)
	}
}

func TestAddRefusesDuplicatesAndNonKubeconfigs(t *testing.T) {
	user := t.TempDir()
	file := filepath.Join(user, "config")
	writeUserFile(t, file, sourceKubeconfig("a"))
	empty := filepath.Join(user, "empty.yaml")
	writeUserFile(t, empty, "apiVersion: v1\nkind: Config\n")
	k := NewKubeconfigSources(filepath.Join(t.TempDir(), "Cogpit"))
	if _, _, err := k.AddFile(file); err != nil {
		t.Fatal(err)
	}
	if _, _, err := k.AddFile(file); err == nil || !strings.Contains(err.Error(), "added already") {
		t.Errorf("a duplicate file: %v", err)
	}
	if _, _, err := k.AddFile(empty); !errors.Is(err, kubeconfig.ErrNoContexts) {
		t.Errorf("a kubeconfig without contexts: %v", err)
	}
	for _, p := range []string{"", filepath.Join(user, "missing"), user} {
		if _, _, err := k.AddFile(p); err == nil {
			t.Errorf("AddFile(%q) succeeded", p)
		}
	}
	for _, p := range []string{"", filepath.Join(user, "missing"), file} {
		if _, err := k.AddFolder(p); err == nil {
			t.Errorf("AddFolder(%q) succeeded", p)
		}
	}
	// Another file of the same name gets its own id.
	other := filepath.Join(user, "sub", "config")
	writeUserFile(t, other, sourceKubeconfig("b"))
	src, _, err := k.AddFile(other)
	if err != nil || src.ID != "config (2)" {
		t.Errorf("second config: %+v %v", src, err)
	}
	if _, err := k.AddFolder(user); err != nil {
		t.Fatal(err)
	}
	src2, err := k.AddFolder(filepath.Join(user, "sub"))
	if err != nil || src2.ID != "sub/" {
		t.Errorf("folder id %+v %v", src2, err)
	}
	if got := uniqueID([]KubeconfigSource{{ID: "x/"}, {ID: "x (2)/"}}, "x/"); got != "x (3)/" {
		t.Errorf("uniqueID = %q", got)
	}
}

// TestExecApprovalStates: a plugin needs an approval, an approval binds it exactly, and a change
// (here an argument) asks again; an approvals file that cannot be read allows nothing.
func TestExecApprovalStates(t *testing.T) {
	user := t.TempDir()
	file := filepath.Join(user, "exec.yaml")
	writeUserFile(t, file, execSourceKubeconfig("e", "aws", "eks", "get-token"))
	cfgDir := filepath.Join(t.TempDir(), "Cogpit")
	k := NewKubeconfigSources(cfgDir)
	if _, _, err := k.AddFile(file); err != nil {
		t.Fatal(err)
	}
	plugin := func() *ExecPluginInfo {
		f, kctx, ok, err := k.Find("e [exec.yaml]")
		if err != nil || !ok {
			t.Fatalf("Find: %v %v", ok, err)
		}
		state, err := k.ExecState(f.Exec[kctx])
		if err != nil {
			t.Fatal(err)
		}
		return &ExecPluginInfo{State: state, Plugin: f.Exec[kctx]}
	}
	p := plugin()
	if p.State != ExecRequired || !errors.Is(k.Policy(p.Plugin), ErrExecNotApproved) {
		t.Fatalf("before approval: %s %v", p.State, k.Policy(p.Plugin))
	}
	if msg := k.Policy(p.Plugin).Error(); !strings.Contains(msg, "aws") || !strings.Contains(msg, file) || strings.Contains(msg, sourceSecret) {
		t.Errorf("refusal %q", msg)
	}
	if err := k.Approve(p.Plugin); err != nil {
		t.Fatal(err)
	}
	checkMode(t, filepath.Join(cfgDir, ExecApprovalsFile), 0o600)
	stored, err := os.ReadFile(filepath.Join(cfgDir, ExecApprovalsFile))
	if err != nil || strings.Contains(string(stored), sourceSecret) {
		t.Fatalf("the approvals file holds the environment's value, or: %v", err)
	}
	if p = plugin(); p.State != ExecApproved || k.Policy(p.Plugin) != nil {
		t.Fatalf("after approval: %s", p.State)
	}

	writeUserFile(t, file, execSourceKubeconfig("e", "aws", "eks", "get-token", "--role", "admin"))
	if p = plugin(); p.State != ExecChanged {
		t.Fatalf("after the arguments changed: %s", p.State)
	}
	var ae *ExecApprovalError
	if err := k.Policy(p.Plugin); !errors.As(err, &ae) || ae.State != ExecChanged || !strings.Contains(err.Error(), "changed since you allowed it") {
		t.Errorf("changed refusal: %v", err)
	}
	if err := k.Approve(p.Plugin); err != nil {
		t.Fatal(err)
	}
	if a, _ := k.approvals(); len(a) != 1 {
		t.Errorf("an approval replaces the earlier one of its file and context: %+v", a)
	}

	if err := os.WriteFile(filepath.Join(cfgDir, ExecApprovalsFile), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	f, kctx, _, _ := k.Find("e [exec.yaml]")
	if err := k.Policy(f.Exec[kctx]); err == nil || errors.Is(err, ErrExecNotApproved) && !strings.Contains(err.Error(), "unreadable") {
		t.Errorf("an unreadable approvals file: %v", err)
	}
}

func TestWritePrivateFileRefusesSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	writeUserFile(t, target, "keep")
	link := filepath.Join(dir, "private", "file")
	if err := os.MkdirAll(filepath.Dir(link), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := writePrivateFile(link, []byte("x")); err == nil {
		t.Error("wrote through a symlink")
	}
	linkedDir := filepath.Join(dir, "linked")
	if err := os.Symlink(filepath.Join(dir, "private"), linkedDir); err != nil {
		t.Fatal(err)
	}
	if err := writePrivateFile(filepath.Join(linkedDir, "new"), []byte("x")); err == nil {
		t.Error("wrote into a symlinked directory")
	}
	if got, _ := os.ReadFile(target); string(got) != "keep" {
		t.Errorf("the target changed: %q", got)
	}
	if err := writePrivateFile(filepath.Join(dir, "private", "ok"), []byte("x")); err != nil {
		t.Fatal(err)
	}
	checkMode(t, filepath.Join(dir, "private", "ok"), 0o600)
}

// TestSyncFolderPicksUpAndDropsFiles: with the watcher running, a kubeconfig dropped into a sync
// folder becomes a context after an EventContexts, and one removed from it disappears after the
// next. The test waits for the events and the listing, never for a fixed time.
func TestSyncFolderPicksUpAndDropsFiles(t *testing.T) {
	folder := t.TempDir()
	k := NewKubeconfigSources(filepath.Join(t.TempDir(), "Cogpit"))
	if _, err := k.AddFolder(folder); err != nil {
		t.Fatal(err)
	}
	s := New(Options{Connector: &ClusterConnector{KubeconfigPaths: []string{filepath.Join(folder, "absent")}, Sources: k}})
	defer s.Close()
	var events atomic.Int64
	defer s.Subscribe(func(ev Event) {
		if ev.Type == EventContexts {
			events.Add(1)
		}
	})()
	stop := s.WatchKubeconfigs(5 * time.Millisecond)
	defer stop()
	id := "dropped [" + filepath.Base(folder) + "/dropped.yaml]"
	has := func() bool {
		infos, _ := s.Contexts()
		return slices.ContainsFunc(infos, func(c ContextInfo) bool { return c.Name == id })
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if has() {
		t.Fatal("listed before it exists")
	}
	writeUserFile(t, filepath.Join(folder, "dropped.yaml"), sourceKubeconfig("dropped"))
	if !testkit.WaitFor(ctx, func() bool { return events.Load() >= 1 }) || !has() {
		t.Fatalf("the new file was not picked up (events %d)", events.Load())
	}
	seen := events.Load()
	if err := os.Remove(filepath.Join(folder, "dropped.yaml")); err != nil {
		t.Fatal(err)
	}
	if !testkit.WaitFor(ctx, func() bool { return events.Load() > seen }) || has() {
		t.Fatalf("the removed file was not dropped (events %d)", events.Load())
	}
	stop()
	stop() // twice is harmless
}

// A folder's files that offer no context are named with a fixed reason, never with their content
// or a parser's message, so that a sync that finds no cluster can say why (owner, 2026-10-12).
func TestSourceStatusNamesSkippedFiles(t *testing.T) {
	dir := t.TempDir()
	writeUserFile(t, filepath.Join(dir, "a.yaml"), sourceKubeconfig("a"))
	writeUserFile(t, filepath.Join(dir, "picture.png"), "\x89PNG\r\n\x1a\n s3cr3t-token-in-a-binary")
	writeUserFile(t, filepath.Join(dir, "empty.yaml"), "apiVersion: v1\nkind: Config\nclusters: []\n")
	k := NewKubeconfigSources(filepath.Join(t.TempDir(), "Cogpit"))
	if _, err := k.AddFolder(dir); err != nil {
		t.Fatal(err)
	}
	_, statuses, err := k.Files()
	if err != nil {
		t.Fatal(err)
	}
	var st SourceStatus
	for _, s := range statuses {
		if s.Kind == SourceFolder {
			st = s
		}
	}
	want := []string{"empty.yaml: defines no context", "picture.png: not a kubeconfig, or unreadable"}
	if st.Skipped != 2 || st.Contexts != 1 || !slices.Equal(st.SkippedFiles, want) {
		t.Errorf("status %+v, want skipped %v", st, want)
	}
	if strings.Contains(strings.Join(st.SkippedFiles, " "), "s3cr3t") {
		t.Error("a skipped file's content reached the status")
	}
}
