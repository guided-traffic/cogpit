package engine

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/guided-traffic/cogpit/internal/testkit"
)

// The default synced folder (~/.kube, G2). Every test names a temporary directory as "the home":
// the engine takes the folder as an option and never looks up the home directory, so none of these
// tests can read the developer's real ~/.kube.

// fakeHome writes a ~/.kube with the user's default kubeconfig, a second kubeconfig, a hidden one
// and a note, and returns the .kube directory.
func fakeHome(t *testing.T) string {
	t.Helper()
	kube := filepath.Join(t.TempDir(), "home", ".kube")
	writeUserFile(t, filepath.Join(kube, "config"), sourceKubeconfig("main"))
	writeUserFile(t, filepath.Join(kube, "team.yaml"), sourceKubeconfig("team"))
	writeUserFile(t, filepath.Join(kube, ".hidden.yaml"), sourceKubeconfig("hidden"))
	writeUserFile(t, filepath.Join(kube, "notes.txt"), "not a kubeconfig\n")
	writeUserFile(t, filepath.Join(kube, ".DS_Store"), "\x00\x00\x00\x01Bud1")
	return kube
}

func contextNames(t *testing.T, k *KubeconfigSources) []string {
	t.Helper()
	infos, err := k.Contexts()
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, c := range infos {
		out = append(out, c.Name)
	}
	return out
}

// TestDefaultFolderIsSyncedUntilRemoved: ~/.kube is a sync folder from the start, listed first, its
// files read on their own (dot files and config too, D-28); the user removes it like any source, the choice is remembered, and no file of it is touched.
func TestDefaultFolderIsSyncedUntilRemoved(t *testing.T) {
	kube := fakeHome(t)
	cfg := filepath.Join(t.TempDir(), "Cogpit")
	open := func() *KubeconfigSources {
		return NewKubeconfigSources(cfg, WithDefaultFolder(kube))
	}
	k := open()
	before := treeState(t, filepath.Dir(kube))

	list, err := k.List()
	if err != nil || len(list) != 1 || !list[0].Default || list[0].ID != ".kube/" || list[0].Kind != SourceFolder || list[0].Path != kube {
		t.Fatalf("sources %+v, %v", list, err)
	}
	if got, want := contextNames(t, k), []string{"hidden [.kube/.hidden.yaml]", "main [.kube/config]", "team [.kube/team.yaml]"}; !slices.Equal(got, want) {
		t.Errorf("contexts %v, want %v", got, want)
	}
	_, statuses, err := k.Files()
	if err != nil || len(statuses) != 1 || statuses[0].Contexts != 3 || statuses[0].Skipped != 1 {
		t.Errorf("statuses %+v, %v", statuses, err)
	}
	if _, err := k.AddFolder(kube); err == nil || !strings.Contains(err.Error(), "added already") {
		t.Errorf("adding the default folder again: %v", err)
	}

	src, err := k.Remove(".kube/")
	if err != nil || !src.Default {
		t.Fatalf("Remove: %+v, %v", src, err)
	}
	for name, k := range map[string]*KubeconfigSources{"now": k, "after a restart": open()} {
		if list, err := k.List(); err != nil || len(list) != 0 {
			t.Errorf("%s: sources %+v, %v", name, list, err)
		}
		if got := contextNames(t, k); len(got) != 0 {
			t.Errorf("%s: contexts %v", name, got)
		}
	}
	sameTree(t, before, treeState(t, filepath.Dir(kube)))

	// Adding it again, by hand, makes it an ordinary source.
	again, err := open().AddFolder(kube)
	if err != nil || again.Default || again.ID != ".kube/" {
		t.Fatalf("re-added: %+v, %v", again, err)
	}
	if got := contextNames(t, open()); len(got) != 3 {
		t.Errorf("contexts after adding it again: %v", got)
	}
}

// TestDefaultFolderOnlyWhileItExists: a machine without ~/.kube lists no source; creating it makes
// the folder appear, and WatchKubeconfigs' signature changes with it.
func TestDefaultFolderOnlyWhileItExists(t *testing.T) {
	kube := filepath.Join(t.TempDir(), ".kube")
	k := NewKubeconfigSources(filepath.Join(t.TempDir(), "Cogpit"), WithDefaultFolder(kube))
	if list, err := k.List(); err != nil || len(list) != 0 {
		t.Fatalf("sources %+v, %v", list, err)
	}
	empty := k.signature()
	writeUserFile(t, filepath.Join(kube, "a.yaml"), sourceKubeconfig("a"))
	if got := contextNames(t, k); !slices.Equal(got, []string{"a [.kube/a.yaml]"}) {
		t.Errorf("contexts %v", got)
	}
	if k.signature() == empty {
		t.Error("the signature did not change when the folder appeared")
	}
}

// TestNoDefaultFolderWithoutTheOption: the engine never reads the home directory by itself. With
// HOME and USERPROFILE pointing at a directory with a .kube full of kubeconfigs, sources made
// without the option see none of it.
func TestNoDefaultFolderWithoutTheOption(t *testing.T) {
	kube := fakeHome(t)
	home := filepath.Dir(kube)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	k := NewKubeconfigSources(filepath.Join(t.TempDir(), "Cogpit"))
	if list, err := k.List(); err != nil || len(list) != 0 {
		t.Errorf("sources %+v, %v", list, err)
	}
	if got := contextNames(t, k); len(got) != 0 {
		t.Errorf("contexts %v", got)
	}
	if WithDefaultFolder("") == nil {
		t.Fatal("no option")
	}
	if list, _ := NewKubeconfigSources(t.TempDir(), WithDefaultFolder("  ")).List(); len(list) != 0 {
		t.Errorf("an empty folder made a source: %+v", list)
	}
}

// TestDefaultFolderPluginsNeedApproval: a file of ~/.kube that names a credential plugin is a
// source file like any other: its plugin runs only after the user allowed it (H-1). That holds for
// config too (D-28), which kubectl alone would run unasked.
func TestDefaultFolderPluginsNeedApproval(t *testing.T) {
	kube := filepath.Join(t.TempDir(), ".kube")
	writeUserFile(t, filepath.Join(kube, "eks.yaml"), execSourceKubeconfig("eks", "aws", "eks", "get-token"))
	k := NewKubeconfigSources(filepath.Join(t.TempDir(), "Cogpit"), WithDefaultFolder(kube))
	infos, err := k.Contexts()
	if err != nil || len(infos) != 1 || infos[0].Exec != ExecRequired {
		t.Fatalf("contexts %+v, %v", infos, err)
	}
	f, kctx, ok, err := k.Find(infos[0].Name)
	if err != nil || !ok {
		t.Fatalf("Find: %v %v", ok, err)
	}
	if err := k.Policy(f.Exec[kctx]); err == nil {
		t.Fatal("the plugin of a ~/.kube file ran without approval")
	}
	if err := k.Approve(f.Exec[kctx]); err != nil {
		t.Fatal(err)
	}
	if err := k.Policy(f.Exec[kctx]); err != nil {
		t.Errorf("approved plugin refused: %v", err)
	}
	// Removing the default folder forgets the approval.
	if _, err := k.Remove(".kube/"); err != nil {
		t.Fatal(err)
	}
	if a, err := k.approvals(); err != nil || len(a) != 0 {
		t.Errorf("approvals after removal: %+v, %v", a, err)
	}
}

// TestDefaultFolderIDDoesNotCollide: a source the user added earlier that is also called ".kube/"
// keeps its id; the default folder takes the next free one.
func TestDefaultFolderIDDoesNotCollide(t *testing.T) {
	kube := filepath.Join(t.TempDir(), "home", ".kube")
	writeUserFile(t, filepath.Join(kube, "a.yaml"), sourceKubeconfig("a"))
	other := filepath.Join(t.TempDir(), ".kube")
	writeUserFile(t, filepath.Join(other, "b.yaml"), sourceKubeconfig("b"))
	cfg := filepath.Join(t.TempDir(), "Cogpit")
	if _, err := NewKubeconfigSources(cfg).AddFolder(other); err != nil {
		t.Fatal(err)
	}
	k := NewKubeconfigSources(cfg, WithDefaultFolder(kube))
	list, err := k.List()
	if err != nil || len(list) != 2 || list[0].ID != ".kube (2)/" || !list[0].Default || list[1].ID != ".kube/" {
		t.Fatalf("sources %+v, %v", list, err)
	}
	if got, want := contextNames(t, k), []string{"a [.kube (2)/a.yaml]", "b [.kube/b.yaml]"}; !slices.Equal(got, want) {
		t.Errorf("contexts %v, want %v", got, want)
	}
}

// TestSyncFolderReadsDotFiles: a hidden kubeconfig in any sync folder is offered, and .DS_Store,
// swap and backup files are not.
func TestSyncFolderReadsDotFiles(t *testing.T) {
	folder := t.TempDir()
	writeUserFile(t, filepath.Join(folder, ".dev.kubeconfig"), sourceKubeconfig("dev"))
	writeUserFile(t, filepath.Join(folder, ".dev.kubeconfig.swp"), sourceKubeconfig("swap"))
	writeUserFile(t, filepath.Join(folder, "old.yaml~"), sourceKubeconfig("backup"))
	k := NewKubeconfigSources(filepath.Join(t.TempDir(), "Cogpit"))
	if _, err := k.AddFolder(folder); err != nil {
		t.Fatal(err)
	}
	want := "dev [" + filepath.Base(folder) + "/.dev.kubeconfig]"
	if got := contextNames(t, k); !slices.Equal(got, []string{want}) {
		t.Errorf("contexts %v, want %v", got, want)
	}
}

// TestConnectLogOfAFailedConnect: a failed connect leaves its attempt's INFO line and the ERROR
// line of the failure; the context is marked, a later success ends with "Connected", and no line
// is empty.
func TestConnectLogOfAFailedConnect(t *testing.T) {
	s, _ := newTestSession(t, testkit.NewMemorySource(), 0)
	if err := s.Connect(t.Context(), "broken"); err == nil {
		t.Fatal("broken connected")
	}
	lines := s.ConnectLog("broken")
	if n := len(lines); n < 2 || lines[0].Level != LogInfo || lines[0].Text != "Connecting to broken" ||
		lines[n-1].Level != LogError || !strings.Contains(lines[n-1].Text, "connection refused") || lines[n-1].Time.IsZero() {
		t.Fatalf("log %+v", lines)
	}
	marked := map[string]bool{}
	infos, _ := s.Contexts()
	for _, c := range infos {
		marked[c.Name] = c.ConnectProblem
	}
	if !marked["broken"] || marked["test"] {
		t.Errorf("marked %v", marked)
	}
	if got := s.ConnectLog("never-tried"); got == nil || len(got) != 0 {
		t.Errorf("log of an untried context: %#v", got)
	}
	if err := s.Connect(t.Context(), "test"); err != nil {
		t.Fatal(err)
	}
	l := s.ConnectLog("test")
	if n := len(l); n < 2 || l[n-1].Level != LogInfo || l[n-1].Text != "Connected to test" || s.state.logs.problem("test") {
		t.Errorf("log of a good connect %+v", l)
	}
	for _, line := range append(lines, l...) {
		if strings.TrimSpace(line.Text) == "" {
			t.Errorf("an empty line: %+v", line)
		}
	}
	attempts, err := s.ConnectAttempts("broken", 0)
	if err != nil || len(attempts) != 1 || attempts[0].Kind != AttemptConnect || attempts[0].Outcome != OutcomeFailed || attempts[0].Running {
		t.Errorf("attempts of broken: %+v, %v", attempts, err)
	}
}

// TestConnectLogIsBoundedAndEscaped: a long, multi-line, control-character error becomes capped,
// escaped lines; an attempt keeps at most maxAttemptLines lines and says how many it left out, a
// context at most maxMemAttempts attempts in memory, and the session at most
// maxConnectLogContexts contexts.
func TestConnectLogIsBoundedAndEscaped(t *testing.T) {
	var l connectLogs
	l.begin("c", AttemptConnect, LogInfo, "start")
	l.add("c", LogError, "first\n\n  second \x1b[31mred\n"+strings.Repeat("x", 5000))
	lines := l.lines("c")
	if len(lines) != 4 || lines[1].Text != "first" || strings.ContainsRune(lines[2].Text, 0x1b) || len(lines[3].Text) > maxConnectLogText+8 {
		t.Fatalf("lines %+v", lines)
	}
	for range 3 * maxAttemptLines {
		l.add("c", LogInfo, "more")
	}
	l.end("c", AttemptConnect, OutcomeOK, LogInfo, "done")
	got, _ := l.attempts("c", 0)
	if len(got) != 1 || len(got[0].Lines) != maxAttemptLines+1 || got[0].Omitted == 0 ||
		!strings.Contains(got[0].Lines[maxAttemptLines].Text, "lines left out") {
		t.Errorf("attempts %d, lines %d, left out %d", len(got), len(got[0].Lines), got[0].Omitted)
	}
	for range 2 * maxMemAttempts {
		l.begin("c", AttemptReconnect, LogWarn, "again")
	}
	if got, _ := l.attempts("c", 0); len(got) != maxMemAttempts {
		t.Errorf("%d attempts kept", len(got))
	}
	for i := range 3 * maxConnectLogContexts {
		l.begin("ctx-"+string(rune('a'+i%26))+string(rune('a'+i/26)), AttemptConnect, LogInfo, "x")
	}
	if len(l.byCtx) > maxConnectLogContexts {
		t.Errorf("%d contexts kept", len(l.byCtx))
	}
}
