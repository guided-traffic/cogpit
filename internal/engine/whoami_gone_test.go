package engine

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/testkit"
)

// listingConnector offers the sources' contexts, as ClusterConnector does with sources.
type listingConnector struct{ sourceConnector }

func (c listingConnector) Contexts() ([]ContextInfo, error) { return c.k.Contexts() }

func writeLastUsers(t *testing.T, dir string, ids ...string) {
	t.Helper()
	doc := lastUsersDoc{Version: lastUsersVersion, Users: map[string]lastUser{}}
	for _, id := range ids {
		doc.Users[id] = lastUser{Username: "alice", Seen: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}
	}
	broken := false
	if err := writePrefsDoc(filepath.Join(dir, LastUsersFile), &broken, doc); err != nil {
		t.Fatal(err)
	}
}

func lastUserIDs(t *testing.T, dir string) []string {
	t.Helper()
	var ids []string
	for id := range readLastUsers(t, dir).Users {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

// Removing a source forgets the last known users of its contexts no source offers any more; the
// same file's context through its folder keeps its user. The sources tell OnRemove of the removed
// source once, after the removal.
func TestRemovingASourceForgetsItsClustersUsers(t *testing.T) {
	f := newTwiceFixture(t)
	var told []KubeconfigSource
	f.k.OnRemove(func(src KubeconfigSource) {
		if list, err := f.k.List(); err != nil || slices.ContainsFunc(list, func(s KubeconfigSource) bool { return s.ID == src.ID }) {
			t.Errorf("told while the source is listed (%v)", err)
		}
		told = append(told, src)
	})
	now := time.Now()
	for _, id := range []string{f.byFile, f.byFolder} {
		f.s.users.seen(id, "alice", now)
	}
	f.s.users.flush(f.s)
	dir := f.s.opts.ClusterSettings.dir
	if got := lastUserIDs(t, dir); len(got) != 2 {
		t.Fatalf("kept before the remove: %v", got)
	}
	if _, err := f.s.RemoveKubeconfigSource(f.fileSource); err != nil {
		t.Fatal(err)
	}
	if got := lastUserIDs(t, dir); !slices.Equal(got, []string{f.byFolder}) {
		t.Errorf("kept after the remove: %v, want only %q", got, f.byFolder)
	}
	if f.s.users.shown(f.byFile, false) != nil {
		t.Error("the removed context still shows its last known user")
	}
	if len(told) != 1 || told[0].ID != f.fileSource {
		t.Errorf("OnRemove told %+v, want the file source once", told)
	}
}

// A source of this run ($KUBECONFIG) is removed for the run only: OnRemove is not told, and its
// contexts keep their last known users for the next start.
func TestRemovingASessionSourceKeepsItsUsers(t *testing.T) {
	file := filepath.Join(t.TempDir(), "session.yaml")
	writeUserFile(t, file, sourceKubeconfig("s"))
	k := NewKubeconfigSources(filepath.Join(t.TempDir(), "Cogpit"), WithSessionFiles([]string{file}))
	list, err := k.List()
	if err != nil || len(list) != 1 || !list[0].Session {
		t.Fatalf("sources %+v (%v)", list, err)
	}
	k.OnRemove(func(src KubeconfigSource) { t.Errorf("told of %+v", src) })
	fc := &fakeConnector{src: testkit.NewMemorySource(), profiles: testProfiles()}
	s := New(Options{Connector: listingConnector{sourceConnector{fakeConnector: fc, k: k}},
		ClusterSettings: NewClusterSettingsStore(filepath.Join(t.TempDir(), "Cogpit"))})
	t.Cleanup(s.Close)
	id := SourceContextID("s", list[0].ID)
	s.users.seen(id, "alice", time.Now())
	s.users.flush(s)
	if _, err := s.RemoveKubeconfigSource(list[0].ID); err != nil {
		t.Fatal(err)
	}
	if got := lastUserIDs(t, s.opts.ClusterSettings.dir); !slices.Equal(got, []string{id}) {
		t.Errorf("kept: %v, want %q", got, id)
	}
}

// At start the last known users of the contexts that are certainly gone go: one of a source no
// longer kept, one whose file was deleted, one a file no longer defines. The contexts the sources
// offer keep theirs; without a list of contexts (no connector) nothing goes on a guess.
func TestStartForgetsTheUsersOfGoneContexts(t *testing.T) {
	folder := t.TempDir()
	writeUserFile(t, filepath.Join(folder, "a.yaml"), sourceKubeconfig("a"))
	k := NewKubeconfigSources(filepath.Join(t.TempDir(), "Cogpit"))
	dirSrc, err := k.AddFolder(folder)
	if err != nil {
		t.Fatal(err)
	}
	label := strings.TrimSuffix(dirSrc.ID, "/")
	offered := SourceContextID("a", label+"/a.yaml")
	undefined := SourceContextID("b", label+"/a.yaml")
	deleted := SourceContextID("a", label+"/deleted.yaml")
	removed := SourceContextID("a", "removed.yaml")
	all := []string{offered, undefined, deleted, removed}
	slices.Sort(all)

	settings := filepath.Join(t.TempDir(), "Cogpit")
	writeLastUsers(t, settings, all...)
	alone := New(Options{ClusterSettings: NewClusterSettingsStore(settings)})
	alone.Close()
	if got := lastUserIDs(t, settings); !slices.Equal(got, all) {
		t.Fatalf("without a connector: %v, want %v", got, all)
	}

	fc := &fakeConnector{src: testkit.NewMemorySource(), profiles: testProfiles()}
	s := New(Options{Connector: listingConnector{sourceConnector{fakeConnector: fc, k: k}},
		ClusterSettings: NewClusterSettingsStore(settings)})
	t.Cleanup(s.Close)
	if got := lastUserIDs(t, settings); !slices.Equal(got, []string{offered}) {
		t.Errorf("kept at start: %v, want only %q", got, offered)
	}
	if _, err := os.Stat(filepath.Join(settings, LastUsersFile+".unreadable")); err == nil {
		t.Error("the file was set aside")
	}
}
