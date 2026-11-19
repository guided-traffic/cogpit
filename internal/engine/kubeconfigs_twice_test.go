package engine

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/testkit"
)

// twiceFixture is a kubeconfig file offered twice: by a file source of its own ("a.yaml") and by
// the synced folder it lies in, as the owner's home folder offered ~/walk-kind.yaml on his walk
// (WF3). The session keeps cluster settings.
type twiceFixture struct {
	s          *Session
	k          *KubeconfigSources
	file       string
	byFile     string // the context id the file source offers
	byFolder   string // the context id the folder offers
	fileSource string
}

func newTwiceFixture(t *testing.T) *twiceFixture {
	t.Helper()
	folder := t.TempDir()
	f := &twiceFixture{file: filepath.Join(folder, "a.yaml")}
	writeUserFile(t, f.file, sourceKubeconfig("a"))
	f.k = NewKubeconfigSources(filepath.Join(t.TempDir(), "Cogpit"))
	dir, err := f.k.AddFolder(folder)
	if err != nil {
		t.Fatal(err)
	}
	src, _, err := f.k.AddFile(f.file)
	if err != nil {
		t.Fatal(err)
	}
	f.fileSource = src.ID
	f.byFile = SourceContextID("a", src.ID)
	f.byFolder = SourceContextID("a", strings.TrimSuffix(dir.ID, "/")+"/a.yaml")
	offered, err := offeredContexts(f.k)
	if err != nil || !offered[f.byFile] || !offered[f.byFolder] {
		t.Fatalf("offered %v (%v), want %q and %q", offered, err, f.byFile, f.byFolder)
	}
	fc := &fakeConnector{src: testkit.NewMemorySource(), profiles: testProfiles()}
	conn := sourceConnector{fakeConnector: fc, k: f.k, file: map[string]string{f.byFile: f.file, f.byFolder: f.file}}
	f.s = New(Options{Connector: conn, SnapshotInterval: 5 * time.Millisecond,
		ClusterSettings: NewClusterSettingsStore(filepath.Join(t.TempDir(), "Cogpit"))})
	t.Cleanup(f.s.Close)
	return f
}

// TestFateOfAsksForTheLabel: with the file source removed, the file is still covered (by the
// folder), but not under the removed source's label.
func TestFateOfAsksForTheLabel(t *testing.T) {
	f := newTwiceFixture(t)
	if got := f.k.FateOf(f.file, ContextSourceLabel(f.byFile)); got != FileOffered {
		t.Fatalf("before the remove: %v", got)
	}
	if _, err := f.k.Remove(f.fileSource); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		label string
		want  FileFate
	}{
		{ContextSourceLabel(f.byFile), FileUnlisted},
		{ContextSourceLabel(f.byFolder), FileOffered},
		{"", FileOffered},
	} {
		if got := f.k.FateOf(f.file, c.label); got != c.want {
			t.Errorf("FateOf(%q) = %v, want %v", c.label, got, c.want)
		}
	}
	if ContextSourceLabel("a") != "" || ContextSourceLabel("kind-dev [.kube/config]") != ".kube/config" ||
		ContextSourceLabel("x [y] [z/w]") != "z/w" {
		t.Error("ContextSourceLabel")
	}
}

// TestRemovedFileSourceClosesItsConnectionThoughAFolderOffersTheFile is WF3: removing the file
// source closes the connection made from it, though the folder still offers the same file as
// another context; a connection made through the folder stays.
func TestRemovedFileSourceClosesItsConnectionThoughAFolderOffersTheFile(t *testing.T) {
	f := newTwiceFixture(t)
	if err := f.s.Connect(context.Background(), f.byFolder); err != nil {
		t.Fatal(err)
	}
	if err := f.s.Connect(context.Background(), f.byFile); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.RemoveKubeconfigSource(f.fileSource); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the file source's connection to close", func() bool {
		return f.s.Backend() == nil && f.s.UIState().Connection.State == Disconnected
	})
	if ui := f.s.UIState(); !strings.Contains(ui.Connection.Error, "source was removed") {
		t.Errorf("reason %q", ui.Connection.Error)
	}
	if !slicesHasBackend(f.s, f.byFolder) {
		t.Errorf("the folder's connection closed too: %v", backendContexts(f.s))
	}
}

// TestRemovingASourceForgetsItsClustersSettings is WF4(b): the removed source's contexts lose their
// cluster settings (re-added, they are new clusters and asked their integrations again); a context
// the sources still offer keeps them, the same file's folder context included.
func TestRemovingASourceForgetsItsClustersSettings(t *testing.T) {
	f := newTwiceFixture(t)
	none := IntegrationSet(0)
	for _, id := range []string{f.byFile, f.byFolder} {
		if _, err := f.s.ChangeClusterSettings(id, func(cs *ClusterSettings) error {
			cs.Name, cs.IntegrationsOff = "named", &none
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.s.RemoveKubeconfigSource(f.fileSource); err != nil {
		t.Fatal(err)
	}
	if cs, _ := f.s.ClusterSettingsOf(f.byFile); !cs.IsZero() {
		t.Errorf("the removed context keeps %+v", cs)
	}
	if _, ok := f.s.UIState().ClusterSettings[f.byFile]; ok {
		t.Error("the UI state still shows the removed context's settings")
	}
	if cs, _ := f.s.ClusterSettingsOf(f.byFolder); cs.Name != "named" || cs.IntegrationsOff == nil {
		t.Errorf("the folder's context lost its settings: %+v", cs)
	}
}

func backendContexts(s *Session) []string {
	var out []string
	for _, b := range s.Backends() {
		out = append(out, b.Context)
	}
	return out
}

func slicesHasBackend(s *Session, id string) bool {
	for _, c := range backendContexts(s) {
		if c == id {
			return true
		}
	}
	return false
}
