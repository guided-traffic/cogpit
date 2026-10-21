package helm

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/guided-traffic/cogpit/internal/testkit"
)

// webChart is the test repository's chart: a ConfigMap from .Values.greeting.
var webChart = testkit.TestChart{
	Name: "web", Version: "0.2.0", AppVersion: "1.2", Description: "A web server",
	Values: "# The greeting the ConfigMap holds.\ngreeting: hello\n",
	Templates: map[string]string{"templates/cm.yaml": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: {{ .Release.Name }}\n" +
		"data:\n  greeting: {{ .Values.greeting | quote }}\n"},
}

func testRepo(t *testing.T) (*testkit.HelmRepo, *Fetcher) {
	t.Helper()
	old := webChart
	old.Version, old.AppVersion = "0.1.0", "1.1"
	pre := webChart
	pre.Version = "0.3.0-rc.1"
	r := testkit.NewHelmRepo(t, old, webChart, pre, testkit.TestChart{Name: "lib", Version: "1.0.0", Type: "library"})
	return r, &Fetcher{Client: r.Client()}
}

// The index is downloaded, parsed with the Helm SDK and kept as charts by name, versions newest
// first; the listed version is the newest that is not a prerelease. A chart is fetched from the
// index's URL, checked against its digest and loaded with its values.yaml, comments included.
func TestDownloadIndexAndFetchChart(t *testing.T) {
	r, f := testRepo(t)
	dest := filepath.Join(t.TempDir(), "cache", "web-index.yaml")
	idx, err := f.DownloadIndex(context.Background(), PublicRepository("test", r.URL), dest)
	if err != nil {
		t.Fatal(err)
	}
	// Windows sets no file modes: there the user profile's permissions protect the cache.
	if fi, err := os.Stat(dest); err != nil || (runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600) {
		t.Errorf("index file %v %v", fi, err)
	}
	if fi, err := os.Stat(filepath.Dir(dest)); err != nil || (runtime.GOOS != "windows" && fi.Mode().Perm() != 0o700) {
		t.Errorf("cache folder %v %v", fi, err)
	}
	if len(idx.Charts) != 2 || idx.Charts[0].Name != "lib" || idx.Charts[1].Name != "web" {
		t.Fatalf("charts %+v", idx.Charts)
	}
	web, _ := idx.Chart("web")
	if len(web.Versions) != 3 || web.Versions[0].Version != "0.3.0-rc.1" || web.Versions[2].Version != "0.1.0" {
		t.Errorf("versions %+v", web.Versions)
	}
	latest, _ := web.Latest()
	if latest.Version != "0.2.0" || latest.AppVersion != "1.2" || latest.Description != "A web server" || latest.Digest == "" {
		t.Errorf("latest %+v", latest)
	}
	again, err := LoadIndex(dest)
	if err != nil || len(again.Charts) != 2 {
		t.Errorf("reloaded %+v %v", again, err)
	}

	c, err := f.FetchChart(context.Background(), PublicRepository("test", r.URL), "web", latest)
	if err != nil {
		t.Fatal(err)
	}
	if !c.Verified || c.Name != "web" || c.Version != "0.2.0" || c.Repository != "test" || len(c.Digest) != 64 {
		t.Errorf("chart %+v", c)
	}
	if c.DefaultValues() != webChart.Values || c.Defaults()["greeting"] != "hello" {
		t.Errorf("values %q %v", c.DefaultValues(), c.Defaults())
	}
	if err := c.checkInstallable(); err != nil {
		t.Error(err)
	}
	libv, _ := idx.Chart("lib")
	lv, _ := libv.Latest()
	lib, err := f.FetchChart(context.Background(), PublicRepository("test", r.URL), "lib", lv)
	if err != nil {
		t.Fatal(err)
	}
	if err := lib.checkInstallable(); err == nil || !strings.Contains(err.Error(), "library") {
		t.Errorf("a library chart: %v", err)
	}
}

// What a repository serves is untrusted: an archive that differs from the index's digest, a chart
// other than the one the index names, an index or archive over the bounds, something that is not
// an index, and a redirect from https to plain http are refused; a failed download keeps the index
// there was. A repository served over plain http is downloaded from, as helm does (Cogpit's
// default, D-29), and may redirect to https.
func TestRepositoryRefusesWhatDoesNotFit(t *testing.T) {
	r, f := testRepo(t)
	dest := filepath.Join(t.TempDir(), "index.yaml")
	idx, err := f.DownloadIndex(context.Background(), PublicRepository("test", r.URL), dest)
	if err != nil {
		t.Fatal(err)
	}
	web, _ := idx.Chart("web")
	v, _ := web.Latest()

	r.Serve("/web-0.2.0.tgz", r.Archive("web-0.1.0.tgz"))
	if _, err := f.FetchChart(context.Background(), PublicRepository("test", r.URL), "web", v); !errors.Is(err, ErrDigestMismatch) {
		t.Errorf("another archive: %v", err)
	}
	unverified := v
	unverified.Digest = ""
	if _, err := f.FetchChart(context.Background(), PublicRepository("test", r.URL), "web", unverified); err == nil || !strings.Contains(err.Error(), "holds chart web 0.1.0") {
		t.Errorf("another version without a digest: %v", err)
	}
	r.Serve("/web-0.2.0.tgz", r.Archive("web-0.2.0.tgz"))
	if c, err := f.FetchChart(context.Background(), PublicRepository("test", r.URL), "web", unverified); err != nil || c.Verified {
		t.Errorf("without a digest: %+v %v", c, err)
	}

	small := &Fetcher{Client: r.Client(), MaxIndex: 64, MaxChart: 64}
	if _, err := small.DownloadIndex(context.Background(), PublicRepository("test", r.URL), dest); err == nil || !strings.Contains(err.Error(), "larger than the 64 B Cogpit downloads") {
		t.Errorf("an index over the bound: %v", err)
	}
	if _, err := small.FetchChart(context.Background(), PublicRepository("test", r.URL), "web", v); err == nil || !strings.Contains(err.Error(), "larger than the 64 B Cogpit downloads") {
		t.Errorf("a chart over the bound: %v", err)
	}
	r.Serve("/index.yaml", []byte("<html>not an index</html>"))
	if _, err := f.DownloadIndex(context.Background(), PublicRepository("test", r.URL), dest); err == nil {
		t.Error("an HTML page parsed as an index")
	}
	if kept, err := LoadIndex(dest); err != nil || len(kept.Charts) != 2 {
		t.Errorf("the index there was: %+v %v", kept, err)
	}
	r.Fail(true)
	if _, err := f.DownloadIndex(context.Background(), PublicRepository("test", r.URL), dest); err == nil || !strings.Contains(err.Error(), "503") {
		t.Errorf("a failing repository: %v", err)
	}

	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("apiVersion: v1\nentries: {}\n")) }))
	defer plain.Close()
	redirect := httptest.NewTLSServer(http.RedirectHandler(plain.URL+"/index.yaml", http.StatusFound))
	defer redirect.Close()
	rf := &Fetcher{Client: redirect.Client()}
	if _, err := rf.DownloadIndex(context.Background(), PublicRepository("r", redirect.URL), dest); !errors.Is(err, ErrNotHTTPS) {
		t.Errorf("a redirect to http: %v", err)
	}
	if idx, err := rf.DownloadIndex(context.Background(), PublicRepository("plain", plain.URL), dest); err != nil || len(idx.Charts) != 0 {
		t.Errorf("an http repository: %+v %v", idx, err)
	}
	r.Fail(false)
	r.Serve("/index.yaml", nil)
	up := httptest.NewServer(http.RedirectHandler(r.URL+"/index.yaml", http.StatusFound))
	defer up.Close()
	if idx, err := f.DownloadIndex(context.Background(), PublicRepository("up", up.URL), dest); err != nil || len(idx.Charts) != 2 {
		t.Errorf("an http repository redirected to https: %+v %v", idx, err)
	}
	relative := v
	relative.URLs = []string{"http://elsewhere.example/web-0.2.0.tgz"}
	if _, err := f.FetchChart(context.Background(), PublicRepository("test", r.URL), "web", relative); !errors.Is(err, ErrNotHTTPS) {
		t.Errorf("a chart URL over http: %v", err)
	}
}

// An index file is untrusted input: hostile content is an error, never a panic.
func FuzzLoadIndex(f *testing.F) {
	f.Add([]byte("apiVersion: v1\nentries:\n  web:\n  - name: web\n    version: 1.0.0\n    urls: [web-1.0.0.tgz]\n"))
	f.Add([]byte("apiVersion: v1\nentries:\n  web: [null]\n"))
	f.Add([]byte(`{"apiVersion":"v1","entries":{"../x":[{"name":"x","version":"1"}]}}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		path := filepath.Join(t.TempDir(), "index.yaml")
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		idx, err := LoadIndex(path)
		if err != nil {
			return
		}
		for _, c := range idx.Charts {
			if !chartNamePattern.MatchString(c.Name) || len(c.Versions) == 0 {
				t.Errorf("chart %+v", c)
			}
		}
	})
}
