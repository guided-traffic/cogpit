package fetch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// testdata/mesa.7z is a 7z archive (LZMA2, made with bsdtar --format 7zip) in the layout of
// mesa-dist-win's: x86/opengl32.dll, x64/libgallium_wgl.dll and x64/opengl32.dll, each a line of
// text naming itself.
func fixture(t *testing.T) (data []byte, sum string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "mesa.7z"))
	if err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256(data)
	return data, hex.EncodeToString(h[:])
}

// serve answers every request with handler on an HTTPS server and returns the archive's URL there.
func serve(t *testing.T, handler http.HandlerFunc) (*httptest.Server, string) {
	t.Helper()
	srv := httptest.NewTLSServer(handler)
	t.Cleanup(srv.Close)
	return srv, srv.URL + "/26.2.4/mesa3d-26.2.4-release-msvc.7z"
}

// names lists dir's files.
func names(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range entries {
		got = append(got, e.Name())
	}
	return got
}

// Install puts the x64 opengl32.dll and libgallium_wgl.dll into the folder, not the x86 one,
// leaves no temporary file behind and reports the download and then the unpacking.
func TestInstallPutsTheTwoFilesInPlace(t *testing.T) {
	data, sum := fixture(t)
	srv, u := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		_, _ = w.Write(data)
	})
	dir := t.TempDir()
	var steps []Progress
	if err := Install(context.Background(), srv.Client(), Source{URL: u, SHA256: sum}, dir, func(p Progress) { steps = append(steps, p) }); err != nil {
		t.Fatal(err)
	}
	if got := names(t, dir); !slices.Equal(got, []string{"libgallium_wgl.dll", "opengl32.dll"}) {
		t.Errorf("the folder holds %q", got)
	}
	for file, want := range map[string]string{"opengl32.dll": "MZ fake x64 opengl32", "libgallium_wgl.dll": "MZ fake x64 libgallium_wgl"} {
		b, err := os.ReadFile(filepath.Join(dir, file))
		if err != nil || !strings.HasPrefix(string(b), want) {
			t.Errorf("%s: %q, %v", file, b, err)
		}
	}
	if len(steps) < 2 || steps[len(steps)-1].Step != Unpacking {
		t.Fatalf("progress: %+v", steps)
	}
	if last := steps[len(steps)-2]; last.Step != Downloading || last.Done != int64(len(data)) || last.Total != int64(len(data)) {
		t.Errorf("the last download report: %+v", last)
	}
}

// An archive of another SHA-256 is never unpacked; the error names the request and both sums, and
// the folder stays empty.
func TestInstallRefusesAnotherArchive(t *testing.T) {
	data, sum := fixture(t)
	other := strings.Repeat("0", 64)
	srv, u := serve(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(data) })
	dir := t.TempDir()
	err := Install(context.Background(), srv.Client(), Source{URL: u, SHA256: other}, dir, nil)
	if err == nil || !strings.Contains(err.Error(), "GET "+u) || !strings.Contains(err.Error(), sum) || !strings.Contains(err.Error(), other) {
		t.Errorf("err = %v", err)
	}
	if got := names(t, dir); len(got) != 0 {
		t.Errorf("the folder holds %q", got)
	}
}

// A failed request's error names the method, the URL and the answer's status; one larger than
// MaxArchive is refused before its body is read.
func TestInstallNamesTheRequest(t *testing.T) {
	_, sum := fixture(t)
	for _, c := range []struct {
		name    string
		handler http.HandlerFunc
		want    string
	}{
		{"not found", func(w http.ResponseWriter, _ *http.Request) { http.NotFound(w, nil) }, ": 404 Not Found"},
		{"too large", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Length", strconv.Itoa(MaxArchive+1))
		}, strconv.Itoa(MaxArchive+1) + " bytes, more than the " + strconv.Itoa(MaxArchive)},
	} {
		srv, u := serve(t, c.handler)
		dir := t.TempDir()
		err := Install(context.Background(), srv.Client(), Source{URL: u, SHA256: sum}, dir, nil)
		if err == nil || !strings.HasPrefix(err.Error(), "GET "+u) || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v", c.name, err)
		}
		if got := names(t, dir); len(got) != 0 {
			t.Errorf("%s: the folder holds %q", c.name, got)
		}
	}
}

// An archive without one of the members puts neither in place.
func TestInstallNeedsBothMembers(t *testing.T) {
	data, sum := fixture(t)
	srv, u := serve(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(data) })
	defer func(m []string) { Members = m }(Members)
	Members = []string{"x64/libgallium_wgl.dll", "x64/opengl32.dll", "x64/dxil.dll"}
	dir := t.TempDir()
	err := Install(context.Background(), srv.Client(), Source{URL: u, SHA256: sum}, dir, nil)
	if err == nil || !strings.Contains(err.Error(), "mesa3d-26.2.4-release-msvc.7z holds no x64/dxil.dll") {
		t.Errorf("err = %v", err)
	}
	if got := names(t, dir); len(got) != 0 {
		t.Errorf("the folder holds %q", got)
	}
}

// A cancelled download ends with the context's error and leaves the folder empty.
func TestInstallStopsWhenCancelled(t *testing.T) {
	_, sum := fixture(t)
	srv, u := serve(t, func(w http.ResponseWriter, _ *http.Request) {})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	dir := t.TempDir()
	if err := Install(ctx, srv.Client(), Source{URL: u, SHA256: sum}, dir, nil); err == nil || !strings.Contains(err.Error(), context.Canceled.Error()) {
		t.Errorf("err = %v", err)
	}
	if got := names(t, dir); len(got) != 0 {
		t.Errorf("the folder holds %q", got)
	}
}

// The client follows a redirect to https only.
func TestClientFollowsHTTPSOnly(t *testing.T) {
	to := func(s string) *http.Request { u, _ := url.Parse(s); return &http.Request{URL: u} }
	if err := httpsOnly(to("https://release-assets.githubusercontent.com/x"), nil); err != nil {
		t.Errorf("https: %v", err)
	}
	if err := httpsOnly(to("http://example.com/x"), nil); err == nil {
		t.Error("a redirect to http was followed")
	}
	if err := httpsOnly(to("https://example.com/x"), make([]*http.Request, 10)); err == nil {
		t.Error("an eleventh redirect was followed")
	}
}

// Writable makes and removes a file; a folder the user may not write to is an error.
func TestWritable(t *testing.T) {
	dir := t.TempDir()
	if err := Writable(dir); err != nil {
		t.Fatal(err)
	}
	if got := names(t, dir); len(got) != 0 {
		t.Errorf("the folder holds %q", got)
	}
	if runtime.GOOS == "windows" {
		t.Skip("a folder's mode does not keep its owner from writing on Windows")
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(dir, 0o700) }()
	if err := Writable(dir); err == nil {
		t.Error("a read-only folder is writable")
	}
}

func TestProgressString(t *testing.T) {
	for _, c := range []struct {
		p    Progress
		want string
	}{
		{Progress{Step: Downloading, Done: 12_900_000, Total: 70_257_286}, "12.3 MiB of 67.0 MiB downloaded"},
		{Progress{Step: Downloading, Done: 512, Total: -1}, "512 B downloaded"},
		{Progress{Step: Unpacking}, "Unpacking opengl32.dll and libgallium_wgl.dll"},
	} {
		if got := c.p.String(); got != c.want {
			t.Errorf("%+v: %q, want %q", c.p, got, c.want)
		}
	}
}
