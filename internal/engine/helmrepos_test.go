package engine

import (
	"context"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	repo "helm.sh/helm/v4/pkg/repo/v1"

	"github.com/guided-traffic/cogpit/internal/helm"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// helmReposOn returns the repositories of a temporary Helm file, downloading from r's server.
func helmReposOn(t *testing.T, r *testkit.HelmRepo, opts ...HelmReposOption) (*HelmRepos, *testkit.HelmRepoFile, string) {
	t.Helper()
	f := testkit.NewHelmRepoFile(t)
	cacheDir := t.TempDir()
	opts = append([]HelmReposOption{WithChartFetcher(&helm.Fetcher{Client: r.Client()})}, opts...)
	return NewHelmRepos(f.Path, cacheDir, opts...), f, cacheDir
}

func webRepo(t *testing.T) *testkit.HelmRepo {
	t.Helper()
	return testkit.NewHelmRepo(t, testkit.TestChart{Name: "web", Version: "0.1.0",
		Templates: map[string]string{"templates/cm.yaml": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: x\n"}})
}

func mustSync(t *testing.T, h *HelmRepos) bool {
	t.Helper()
	changed, err := h.Sync()
	if err != nil {
		t.Fatal(err)
	}
	return changed
}

// What changed in an entry decides what Cogpit keeps: new credentials keep the index but download
// it again at once (fetchNow); a new URL never shows the old URL's index (another cache key); a
// removed entry leaves; an unchanged file is not read again.
func TestHelmRepoChangesTakenOver(t *testing.T) {
	r := webRepo(t)
	h, f, cacheDir := helmReposOn(t, r)
	f.Write(r.Entry("test"))
	if !mustSync(t, h) {
		t.Fatal("the first read changed nothing")
	}
	if mustSync(t, h) {
		t.Error("an unchanged file changed the list")
	}
	if _, err := h.Update(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	first := h.state["test"]
	if first.index == nil || first.fetchNow {
		t.Fatalf("after the update: %+v", first)
	}

	f.Write(&repo.Entry{Name: "test", URL: r.URL, Username: "u", Password: "p-91"})
	if !mustSync(t, h) {
		t.Fatal("new credentials changed nothing")
	}
	st := h.state["test"]
	if st == first || st.index == nil || !st.fetchNow || st.key != first.key || st.since != first.since {
		t.Errorf("new credentials: %+v", st)
	}
	if l, _ := h.List(); !l.Repositories[0].HasCredentials || l.Repositories[0].Charts != 1 {
		t.Errorf("listed %+v", l.Repositories)
	}

	f.Write(&repo.Entry{Name: "test", URL: r.URL + "/moved"})
	mustSync(t, h)
	st = h.state["test"]
	if st.index != nil || st.key == first.key || st.since == first.since || !st.fetchNow {
		t.Errorf("a new URL: %+v", st)
	}
	if objs := h.chartObjects(); len(objs) != 0 {
		t.Errorf("the old URL's charts stay: %v", objs)
	}
	if got, _ := os.ReadDir(filepath.Join(cacheDir, "helm", "indexes")); len(got) != 1 {
		t.Errorf("the cache holds %d indexes", len(got))
	}

	f.Write()
	mustSync(t, h)
	if len(h.state) != 0 || len(h.entries) != 0 {
		t.Errorf("removed: %+v %+v", h.state, h.entries)
	}
}

// A file seen changing is read only once it stayed the same for the settle time: one written
// while Cogpit waited is left for the next look, which takes it.
func TestHelmRepoFileReadWhenSettled(t *testing.T) {
	r := webRepo(t)
	h, f, _ := helmReposOn(t, r, WithRepositoryFilePoll(400*time.Millisecond)) // settle 100 ms
	f.Write(r.Entry("a"))
	mustSync(t, h)
	f.Write(r.Entry("a"), r.Entry("b"))
	h.settling = func() { f.Write(r.Entry("a"), r.Entry("b"), r.Entry("c")) } // written while Cogpit waits
	if mustSync(t, h) {
		t.Error("a file written during the settle time was taken")
	}
	h.settling = nil
	if l, _ := h.List(); len(l.Repositories) != 1 {
		t.Errorf("listed %+v", l.Repositories)
	}
	if !mustSync(t, h) {
		t.Fatal("the settled file was not taken")
	}
	if l, _ := h.List(); len(l.Repositories) != 3 {
		t.Errorf("listed %+v", l.Repositories)
	}
}

// A read that did not reach Helm's file (on Windows: the file held by another program for longer
// than heldRetry) is not taken for the file: the list says why, and the next look reads the file
// again though it did not change. A missing file, one read and one that does not parse are read
// again only once they change.
func TestHelmRepoFileReadAgainAfterAFailedRead(t *testing.T) {
	r := webRepo(t)
	h, f, _ := helmReposOn(t, r, WithRepositoryFilePoll(20*time.Millisecond)) // settle 5 ms
	reads := 0
	var fail error
	h.read = func(path string) ([]helm.RepositoryEntry, error) {
		reads++
		if fail != nil {
			return nil, fail
		}
		return helm.ReadRepositoryFile(path)
	}
	readOnce := func(what string) {
		t.Helper()
		mustSync(t, h)
		n := reads
		mustSync(t, h)
		if reads != n {
			t.Errorf("%s was read again unchanged", what)
		}
	}
	readOnce("a missing file")

	f.Write(r.Entry("test"))
	fail = &fs.PathError{Op: "open", Path: f.Path, Err: errors.New("held by another program")}
	mustSync(t, h)
	if l, _ := h.List(); l.Status != HelmFileError || !strings.Contains(l.Error, "held by another program") || len(l.Repositories) != 0 {
		t.Fatalf("after a failed read: %+v", l)
	}
	fail = nil
	if !mustSync(t, h) {
		t.Fatal("the file was not read again after a failed read")
	}
	if l, _ := h.List(); l.Status != HelmFileOK || len(l.Repositories) != 1 {
		t.Fatalf("after the next read: %+v", l)
	}
	readOnce("a file read")

	f.WriteText("repositories: [unclosed\n")
	readOnce("a file that does not parse")
	if l, _ := h.List(); l.Status != HelmFileError || len(l.Repositories) != 1 {
		t.Errorf("a file that does not parse: %+v", l)
	}
}

// A repository removed from Helm's file while its index downloads: the download stops and its
// index is dropped.
func TestHelmRepoRemovedWhileDownloading(t *testing.T) {
	entered := make(chan struct{}, 1)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		select {
		case entered <- struct{}{}:
		default:
		}
		<-req.Context().Done()
	}))
	defer srv.Close()
	f := testkit.NewHelmRepoFile(t)
	h := NewHelmRepos(f.Path, t.TempDir(), WithChartFetcher(&helm.Fetcher{Client: srv.Client()}))
	f.Write(&repo.Entry{Name: "slow", URL: srv.URL})
	mustSync(t, h)
	errc := make(chan error, 1)
	go func() { _, err := h.Update(context.Background(), []string{"slow"}); errc <- err }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the download did not start")
	}
	f.Write()
	mustSync(t, h)
	select {
	case err := <-errc:
		if !errors.Is(err, errRepoChanged) && (err == nil || !strings.Contains(err.Error(), "changed")) {
			t.Errorf("the stopped download: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the download of a removed repository did not stop")
	}
	if len(h.state) != 0 {
		t.Errorf("state %+v", h.state)
	}
}
