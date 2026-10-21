package engine

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The default kubeconfig as sources (D-28, 2026-10-12). Every test names temporary directories as
// the home and as the files $KUBECONFIG lists: the engine takes both as options and never looks at
// the environment, so none of these tests can read the developer's real ~/.kube or $KUBECONFIG.

// sessionSetup is a fake home with a ~/.kube/config and two kubeconfig files elsewhere that
// "$KUBECONFIG" names, and the sources over them.
type sessionSetup struct {
	kube, dir     string // ~/.kube and the directory of the other files
	a, b          string // the files $KUBECONFIG names
	cfg           string // Cogpit's configuration directory
	open          func(extra ...string) *KubeconfigSources
	treeBefore    map[string]fileState
	treeBeforeDir string
}

func newSessionSetup(t *testing.T) *sessionSetup {
	t.Helper()
	root := t.TempDir()
	su := &sessionSetup{kube: filepath.Join(root, "home", ".kube"), dir: filepath.Join(root, "work"), cfg: filepath.Join(t.TempDir(), "Cogpit")}
	su.a, su.b = filepath.Join(su.dir, "a.yaml"), filepath.Join(su.dir, "b.yaml")
	writeUserFile(t, filepath.Join(su.kube, "config"), sourceKubeconfig("main"))
	writeUserFile(t, su.a, sourceKubeconfig("alpha"))
	writeUserFile(t, su.b, sourceKubeconfig("beta"))
	su.treeBefore, su.treeBeforeDir = treeState(t, root), root
	su.open = func(extra ...string) *KubeconfigSources {
		return NewKubeconfigSources(su.cfg, WithDefaultFolder(su.kube), WithSessionFiles(append([]string{su.a, su.b}, extra...)))
	}
	return su
}

func (su *sessionSetup) untouched(t *testing.T) {
	t.Helper()
	sameTree(t, su.treeBefore, treeState(t, su.treeBeforeDir))
}

func sourceIDs(t *testing.T, k *KubeconfigSources) []string {
	t.Helper()
	list, err := k.List()
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, s := range list {
		ids = append(ids, s.ID)
	}
	return ids
}

// TestDefaultKubeconfigIsOneFileOfTheSyncedFolder: ~/.kube/config is offered as the context
// "main [.kube/config]" of the default sync folder, read on its own. Removing the folder hides it,
// and a $KUBECONFIG file lying in the folder does not bring it back, as Lens behaves.
func TestDefaultKubeconfigIsOneFileOfTheSyncedFolder(t *testing.T) {
	su := newSessionSetup(t)
	k := su.open(filepath.Join(su.kube, "config")) // $KUBECONFIG=~/.kube/config, the common case
	if got, want := contextNames(t, k), []string{"alpha [a.yaml]", "beta [b.yaml]", "main [.kube/config]"}; !slices.Equal(got, want) {
		t.Fatalf("contexts %v, want %v", got, want)
	}
	if got, want := sourceIDs(t, k), []string{".kube/", "a.yaml", "b.yaml"}; !slices.Equal(got, want) {
		t.Fatalf("sources %v, want %v (config is the folder's, not a source of its own)", got, want)
	}
	if _, err := k.Remove(".kube/"); err != nil {
		t.Fatal(err)
	}
	for name, k := range map[string]*KubeconfigSources{"now": k, "after a restart": su.open(filepath.Join(su.kube, "config"))} {
		if got, want := contextNames(t, k), []string{"alpha [a.yaml]", "beta [b.yaml]"}; !slices.Equal(got, want) {
			t.Errorf("%s: contexts %v, want %v", name, got, want)
		}
	}
	su.untouched(t)
}

// TestSessionFilesAreSourcesOfTheirOwn: the files $KUBECONFIG and --kubeconfig name are sources
// read alone, removable for the session only, never stored; a missing file, /dev/null and a
// directory are not offered.
func TestSessionFilesAreSourcesOfTheirOwn(t *testing.T) {
	su := newSessionSetup(t)
	missing := filepath.Join(su.dir, "missing.yaml")
	k := su.open(missing, os.DevNull, su.dir, su.a) // su.a twice
	list, err := k.List()
	if err != nil || len(list) != 3 {
		t.Fatalf("sources %+v, %v", list, err)
	}
	if list[0].Session || !list[0].Default || !list[1].Session || !list[2].Session || list[1].Kind != SourceFile || list[1].Path != su.a {
		t.Errorf("sources %+v", list)
	}
	_, statuses, err := k.Files()
	if err != nil || statuses[1].Contexts != 1 || statuses[1].Error != "" {
		t.Errorf("statuses %+v, %v", statuses, err)
	}

	if src, err := k.Remove("a.yaml"); err != nil || !src.Session {
		t.Fatalf("Remove: %+v, %v", src, err)
	}
	if got, want := contextNames(t, k), []string{"beta [b.yaml]", "main [.kube/config]"}; !slices.Equal(got, want) {
		t.Errorf("after removing a.yaml: %v, want %v", got, want)
	}
	if k.Fate(su.a) != FileUnlisted {
		t.Error("a removed session file is still covered")
	}
	// Nothing was stored: the sources file does not exist, and the next start offers the file again.
	if _, err := os.Stat(filepath.Join(su.cfg, KubeconfigSourcesFile)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a session file was stored: %v", err)
	}
	if got := contextNames(t, su.open()); len(got) != 3 {
		t.Errorf("after a restart: %v", got)
	}
	su.untouched(t)
}

// TestSessionFileCanBeAddedForGood: adding a file that is offered for this run only stores it, and
// it stays one source (not twice).
func TestSessionFileCanBeAddedForGood(t *testing.T) {
	su := newSessionSetup(t)
	k := su.open()
	src, _, err := k.AddFile(su.a)
	if err != nil || src.Session || src.ID != "a.yaml" {
		t.Fatalf("AddFile: %+v, %v", src, err)
	}
	if got, want := sourceIDs(t, k), []string{".kube/", "a.yaml", "b.yaml"}; !slices.Equal(got, want) {
		t.Errorf("sources %v, want %v", got, want)
	}
	// Without $KUBECONFIG it is still there: it is the user's now.
	if got := contextNames(t, NewKubeconfigSources(su.cfg)); !slices.Equal(got, []string{"alpha [a.yaml]"}) {
		t.Errorf("stored source: %v", got)
	}
}

// TestSameNamedContextsInTwoFilesAreBothReachable: ids carry the file, so two files that define
// "prod" give two contexts; a bare name is resolved only when one file has it.
func TestSameNamedContextsInTwoFilesAreBothReachable(t *testing.T) {
	su := newSessionSetup(t)
	writeUserFile(t, su.a, strings.ReplaceAll(sourceKubeconfig("alpha"), "alpha", "prod"))
	writeUserFile(t, su.b, strings.ReplaceAll(sourceKubeconfig("beta"), "beta", "prod"))
	k := su.open()
	c := &ClusterConnector{Sources: k, IncludeDemo: true}
	infos, err := c.Contexts()
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, i := range infos {
		names = append(names, i.Name)
		if i.Name == "prod [a.yaml]" && (i.KubeContext != "prod" || i.Source != "a.yaml" || i.File != su.a || i.Server != "https://prod.example:6443") {
			t.Errorf("prod [a.yaml] = %+v", i)
		}
	}
	if want := []string{"main [.kube/config]", "prod [a.yaml]", "prod [b.yaml]", "demo"}; !slices.Equal(names, want) {
		t.Fatalf("contexts %v, want %v", names, want)
	}
	for _, id := range []string{"prod [a.yaml]", "prod [b.yaml]"} {
		f, kctx, ok, err := k.Find(id)
		if err != nil || !ok || kctx != "prod" || f.Label != id[len("prod ["):len(id)-1] {
			t.Errorf("Find(%q) = %+v %q %v %v", id, f, kctx, ok, err)
		}
		if got, err := c.ResolveContext(id); err != nil || got != id {
			t.Errorf("ResolveContext(%q) = %q, %v", id, got, err)
		}
	}
	// A bare name two files share is refused with the ids to choose from; a unique one resolves; the
	// demo cluster stays "demo".
	if _, err := c.ResolveContext("prod"); err == nil || !strings.Contains(err.Error(), "prod [a.yaml]") || !strings.Contains(err.Error(), "prod [b.yaml]") {
		t.Errorf("ambiguous bare name: %v", err)
	}
	if got, err := c.ResolveContext("main"); err != nil || got != "main [.kube/config]" {
		t.Errorf("ResolveContext(main) = %q, %v", got, err)
	}
	if got, err := c.ResolveContext("demo"); err != nil || got != "demo" {
		t.Errorf("ResolveContext(demo) = %q, %v", got, err)
	}
	if got, err := c.ResolveContext("nothing"); err != nil || got != "nothing" {
		t.Errorf("ResolveContext(nothing) = %q, %v", got, err)
	}
	// Connecting a name no source has fails by name, not by falling back to a merged kubeconfig.
	if _, err := c.Connect(t.Context(), "nothing"); err == nil || !strings.Contains(err.Error(), "no such context") {
		t.Errorf("Connect(nothing): %v", err)
	}
	if _, err := c.Connect(t.Context(), "prod"); err == nil || !strings.Contains(err.Error(), "several kubeconfig files") {
		t.Errorf("Connect(prod): %v", err)
	}
}

// TestADemoNamedContextDoesNotClashWithTheDemo: a file's context called "demo" is "demo [file]".
func TestADemoNamedContextDoesNotClashWithTheDemo(t *testing.T) {
	su := newSessionSetup(t)
	writeUserFile(t, su.a, strings.ReplaceAll(sourceKubeconfig("alpha"), "alpha", "demo"))
	c := &ClusterConnector{Sources: su.open(), IncludeDemo: true}
	infos, err := c.Contexts()
	if err != nil {
		t.Fatal(err)
	}
	var demos, shadowed int
	for _, i := range infos {
		if i.Demo {
			demos++
		}
		if i.Name == "demo [a.yaml]" {
			shadowed++
		}
	}
	if demos != 1 || shadowed != 1 {
		t.Errorf("contexts %+v", infos)
	}
}

// TestConfigWithAPluginAsksForApprovalOnce: a plugin in ~/.kube/config asks for the user's
// approval like a plugin in any file (H-1): the connect is refused with ErrExecNotApproved, the
// user approves once through the session, and the approval survives a restart; a changed plugin
// asks again. The file is never written.
func TestConfigWithAPluginAsksForApprovalOnce(t *testing.T) {
	su := newSessionSetup(t)
	writeUserFile(t, filepath.Join(su.kube, "config"), execSourceKubeconfig("eks", "aws", "eks", "get-token"))
	su.treeBefore = treeState(t, su.treeBeforeDir)
	const id = "eks [.kube/config]"
	open := func() (*Session, *ClusterConnector) {
		c := &ClusterConnector{Sources: su.open(filepath.Join(su.kube, "config"))}
		return New(Options{Connector: c}), c
	}
	s, _ := open()
	defer s.Close()

	infos, _ := s.Contexts()
	if i := slices.IndexFunc(infos, func(c ContextInfo) bool { return c.Name == id }); i < 0 || infos[i].Exec != ExecRequired {
		t.Fatalf("contexts %+v", infos)
	}
	// By its id and by its bare name, an unapproved plugin is not run.
	for _, name := range []string{id, "eks"} {
		if err := s.Connect(t.Context(), name); !errors.Is(err, ErrExecNotApproved) {
			t.Errorf("Connect(%q) = %v, want ErrExecNotApproved", name, err)
		}
	}
	info, err := s.ExecPlugin(id)
	if err != nil || info.State != ExecRequired || info.Plugin.Command != "aws" {
		t.Fatalf("ExecPlugin: %+v, %v", info, err)
	}
	if _, err := s.ApproveExecAll([]ExecApprovalItem{{Context: id, Fingerprint: "sha256:other"}}); !errors.Is(err, ErrExecChangedSinceShown) {
		t.Errorf("approving a plugin other than the one shown: %v", err)
	}
	if _, err := s.ApproveExecAll([]ExecApprovalItem{{Context: id, Fingerprint: info.Fingerprint}}); err != nil {
		t.Fatal(err)
	}

	s2, _ := open() // a restart: asked once, remembered
	defer s2.Close()
	if info, err := s2.ExecPlugin(id); err != nil || info.State != ExecApproved {
		t.Fatalf("after a restart: %+v, %v", info, err)
	}
	writeUserFile(t, filepath.Join(su.kube, "config"), execSourceKubeconfig("eks", "aws", "eks", "get-token", "--role", "admin"))
	if info, err := s2.ExecPlugin(id); err != nil || info.State != ExecChanged {
		t.Errorf("after the plugin changed: %+v, %v", info, err)
	}
}

// TestRemovingAPluginSessionFileKeepsItsApproval: the approvals of a file removed for the session
// are kept (it is back at the next start); those of a removed stored source are forgotten.
func TestRemovingAPluginSessionFileKeepsItsApproval(t *testing.T) {
	su := newSessionSetup(t)
	writeUserFile(t, su.a, execSourceKubeconfig("alpha", "aws", "eks", "get-token"))
	k := su.open()
	f, kctx, ok, err := k.Find("alpha [a.yaml]")
	if err != nil || !ok {
		t.Fatalf("Find: %v %v", ok, err)
	}
	if err := k.Approve(f.Exec[kctx]); err != nil {
		t.Fatal(err)
	}
	if _, err := k.Remove("a.yaml"); err != nil {
		t.Fatal(err)
	}
	if a, err := k.approvals(); err != nil || len(a) != 1 {
		t.Errorf("approvals after removing a session file: %+v, %v", a, err)
	}
}

// TestStartWithoutKubeFolderOrKubeconfigFiles: no ~/.kube and a $KUBECONFIG of missing files is a
// normal start: no sources, no contexts, no error, and nothing is created.
func TestStartWithoutKubeFolderOrKubeconfigFiles(t *testing.T) {
	root := t.TempDir()
	cfg := filepath.Join(root, "Cogpit")
	k := NewKubeconfigSources(cfg, WithDefaultFolder(filepath.Join(root, "home", ".kube")),
		WithSessionFiles([]string{filepath.Join(root, "nope.yaml"), os.DevNull}))
	c := &ClusterConnector{Sources: k, IncludeDemo: true}
	infos, err := c.Contexts()
	if err != nil || len(infos) != 1 || !infos[0].Demo {
		t.Errorf("contexts %+v, %v", infos, err)
	}
	if list, err := k.List(); err != nil || len(list) != 0 {
		t.Errorf("sources %+v, %v", list, err)
	}
	if entries, _ := os.ReadDir(root); len(entries) != 0 {
		t.Errorf("start created %v", entries)
	}
	// The folder and the file appear later: the signature changes and they are offered.
	before := k.signature()
	writeUserFile(t, filepath.Join(root, "nope.yaml"), sourceKubeconfig("late"))
	if k.signature() == before {
		t.Error("the signature did not change when a session file appeared")
	}
	if got := contextNames(t, k); !slices.Equal(got, []string{"late [nope.yaml]"}) {
		t.Errorf("contexts %v", got)
	}
}

// TestEarlierSourcesFileIsReadAsItIs: a kubeconfigs.json from before D-28 (a stored file, a stored
// folder, a pasted entry and the removed default folder) is read unchanged, nothing is deleted, and
// reading rewrites neither it nor any kubeconfig file. Whoever removed ~/.kube earlier now also
// loses config's contexts, as Lens would show them.
func TestEarlierSourcesFileIsReadAsItIs(t *testing.T) {
	su := newSessionSetup(t)
	folder := filepath.Join(su.dir, "team")
	writeUserFile(t, filepath.Join(folder, "t.yaml"), sourceKubeconfig("team"))
	su.treeBefore = treeState(t, su.treeBeforeDir)
	quote := func(path string) string { q, _ := json.Marshal(path); return string(q) } // a Windows path's backslashes escaped
	doc := `{"version":1,"defaultFolderRemoved":true,"sources":[` +
		`{"id":"a.yaml","kind":"file","path":` + quote(su.a) + `,"added":"2026-10-01T00:00:00Z"},` +
		`{"id":"team/","kind":"folder","path":` + quote(folder) + `,"added":"2026-10-01T00:00:00Z"},` +
		`{"id":"pasted-1","kind":"pasted","path":` + quote(filepath.Join(su.cfg, "kubeconfigs", "p.yaml")) + `,"added":"2026-10-01T00:00:00Z"}]}`
	writeUserFile(t, filepath.Join(su.cfg, KubeconfigSourcesFile), doc)
	if err := os.Chmod(filepath.Join(su.cfg, KubeconfigSourcesFile), 0o600); err != nil {
		t.Fatal(err)
	}
	cfgBefore := treeState(t, su.cfg)

	k := su.open() // KUBECONFIG=a.yaml,b.yaml
	if got, want := sourceIDs(t, k), []string{"a.yaml", "team/", "b.yaml"}; !slices.Equal(got, want) {
		t.Errorf("sources %v, want %v", got, want)
	}
	if got, want := contextNames(t, k), []string{"alpha [a.yaml]", "beta [b.yaml]", "team [team/t.yaml]"}; !slices.Equal(got, want) {
		t.Errorf("contexts %v, want %v", got, want)
	}
	su.untouched(t)
	sameTree(t, cfgBefore, treeState(t, su.cfg))
}
