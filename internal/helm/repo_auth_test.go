package helm

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/testkit"
)

// Test credentials: each test looks for them in every error it gets.
const (
	repoUser = "alice-chart-user"
	repoPass = "pw-repo-51"
)

// readEntry writes Helm's repositories file with text and returns its only entry's Repository.
func readEntry(t *testing.T, text string) *Repository {
	t.Helper()
	path := filepath.Join(t.TempDir(), "repositories.yaml")
	writeFile(t, path, "apiVersion: \"\"\nrepositories:\n"+text)
	entries, err := ReadRepositoryFile(path)
	if err != nil || len(entries) != 1 || entries[0].Repository == nil {
		t.Fatalf("entries %+v %v", entries, err)
	}
	return entries[0].Repository
}

// caFile writes a repository server's certificate as a CA file.
func caFile(t *testing.T, pemBytes []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(path, pemBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// noSecrets fails the test when err's text holds a credential.
func noSecrets(t *testing.T, what string, err error, secrets ...string) {
	t.Helper()
	if err == nil {
		return
	}
	for _, s := range append([]string{repoPass, repoUser}, secrets...) {
		if strings.Contains(err.Error(), s) {
			t.Errorf("%s: the error quotes a credential: %v", what, err)
		}
	}
}

// An https repository behind a login, trusted through its entry's CA file, as Helm's file names
// it: the index and the chart download with the basic auth, the chart installs through the SDK.
// Without the credentials, with a wrong password or without the CA file it fails, and no error
// quotes a credential.
func TestAuthenticatedTLSRepository(t *testing.T) {
	r := testkit.NewHelmRepo(t, webChart)
	r.RequireBasicAuth(repoUser, repoPass)
	ca := caFile(t, r.CertificatePEM())
	repo := readEntry(t, fmt.Sprintf("- name: private\n  url: %s\n  username: %s\n  password: %s\n  caFile: %s\n", r.URL, repoUser, repoPass, ca))
	f := &Fetcher{} // the default client: only the CA file makes the test server trusted
	dest := filepath.Join(t.TempDir(), "index.yaml")
	idx, err := f.DownloadIndex(context.Background(), repo, dest)
	if err != nil {
		t.Fatal(err)
	}
	web, _ := idx.Chart("web")
	v, _ := web.Latest()
	ch, err := f.FetchChart(context.Background(), repo, "web", v)
	if err != nil || !ch.Verified || ch.Repository != "private" {
		t.Fatalf("chart %+v %v", ch, err)
	}
	if r.Authorized("/index.yaml") != 1 || r.Authorized("/web-0.2.0.tgz") != 1 {
		t.Errorf("authorized requests: index %d, chart %d", r.Authorized("/index.yaml"), r.Authorized("/web-0.2.0.tgz"))
	}
	a, store := installFixture(t)
	if rev, err := a.Install(context.Background(), InstallRequest{Namespace: "apps", Name: "web", Chart: ch, Values: ch.Defaults()}); err != nil || rev != 1 {
		t.Fatalf("install: %d %v", rev, err)
	}
	if rls := last(t, store); rls.Chart.Metadata.Version != "0.2.0" {
		t.Errorf("installed %s", rls.Chart.Metadata.Version)
	}

	anonymous := readEntry(t, fmt.Sprintf("- name: private\n  url: %s\n  caFile: %s\n", r.URL, ca))
	_, err = f.DownloadIndex(context.Background(), anonymous, dest)
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Errorf("without credentials: %v", err)
	}
	wrong := readEntry(t, fmt.Sprintf("- name: private\n  url: %s\n  username: %s\n  password: wrong-pw-52\n  caFile: %s\n", r.URL, repoUser, ca))
	_, err = f.DownloadIndex(context.Background(), wrong, dest)
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Errorf("a wrong password: %v", err)
	}
	noSecrets(t, "a wrong password", err, "wrong-pw-52")
	untrusted := readEntry(t, fmt.Sprintf("- name: private\n  url: %s\n  username: %s\n  password: %s\n", r.URL, repoUser, repoPass))
	_, err = f.DownloadIndex(context.Background(), untrusted, dest)
	if err == nil || !strings.Contains(err.Error(), "certificate") {
		t.Errorf("without the CA file: %v", err)
	}
	noSecrets(t, "without the CA file", err)
	missingCA := readEntry(t, fmt.Sprintf("- name: private\n  url: %s\n  caFile: %s\n", r.URL, filepath.Join(t.TempDir(), "gone.pem")))
	if _, err := f.DownloadIndex(context.Background(), missingCA, dest); err == nil || !strings.Contains(err.Error(), "gone.pem") {
		t.Errorf("a missing CA file: %v", err)
	}
	if kept, err := LoadIndex(dest); err != nil || len(kept.Charts) != 1 {
		t.Errorf("the failed downloads replaced the index: %+v %v", kept, err)
	}
}

// recorder is a TLS server that records whether each request carried an Authorization header.
type recorder struct {
	srv  *httptest.Server
	mu   sync.Mutex
	auth []bool
}

func newRecorder(t *testing.T, h http.HandlerFunc) *recorder {
	t.Helper()
	rec := &recorder{}
	rec.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		rec.mu.Lock()
		rec.auth = append(rec.auth, req.Header.Get("Authorization") != "")
		rec.mu.Unlock()
		h(w, req)
	}))
	t.Cleanup(rec.srv.Close)
	return rec
}

func (rec *recorder) sawAuth() (requests, authorized int) {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	for _, a := range rec.auth {
		if a {
			authorized++
		}
	}
	return len(rec.auth), authorized
}

const emptyIndex = "apiVersion: v1\nentries: {}\n"

// A redirect to another host or port gets no Authorization, a subdomain included (Go's client
// would keep it for a subdomain); with pass_credentials_all it does.
func TestCredentialsStayOnTheRepositoryHost(t *testing.T) {
	for _, all := range []bool{false, true} {
		other := newRecorder(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, emptyIndex) })
		repoSrv := newRecorder(t, func(w http.ResponseWriter, req *http.Request) {
			http.Redirect(w, req, other.srv.URL+"/index.yaml", http.StatusFound)
		})
		repo := PublicRepository("r", repoSrv.srv.URL).WithBasicAuth(repoUser, repoPass)
		repo.PassCredentialsAll = all
		f := &Fetcher{Client: repoSrv.srv.Client()} // httptest's TLS servers share one certificate
		if _, err := f.DownloadIndex(context.Background(), repo, filepath.Join(t.TempDir(), "i.yaml")); err != nil {
			t.Fatal(err)
		}
		if n, a := repoSrv.sawAuth(); n != 1 || a != 1 {
			t.Errorf("all=%v: the repository saw %d requests, %d with credentials", all, n, a)
		}
		want := 0
		if all {
			want = 1
		}
		if n, a := other.sawAuth(); n != 1 || a != want {
			t.Errorf("all=%v: the other port saw %d requests, %d with credentials", all, n, a)
		}
	}

	// A subdomain: example.com redirects to sub.example.com (httptest's certificate holds both).
	other := newRecorder(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, emptyIndex) })
	_, otherPort, _ := net.SplitHostPort(other.srv.Listener.Addr().String())
	repoSrv := newRecorder(t, func(w http.ResponseWriter, req *http.Request) {
		http.Redirect(w, req, "https://sub.example.com:"+otherPort+"/index.yaml", http.StatusFound)
	})
	_, repoPort, _ := net.SplitHostPort(repoSrv.srv.Listener.Addr().String())
	tr := repoSrv.srv.Client().Transport.(*http.Transport).Clone()
	tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		_, port, _ := net.SplitHostPort(addr)
		return (&net.Dialer{}).DialContext(ctx, network, "127.0.0.1:"+port)
	}
	client := &http.Client{Transport: tr}
	// Go's own rule, for comparison: a header set on the request follows to the subdomain.
	req, _ := http.NewRequest(http.MethodGet, "https://example.com:"+repoPort+"/index.yaml", nil)
	req.SetBasicAuth(repoUser, repoPass)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if _, a := other.sawAuth(); a != 1 {
		t.Fatalf("Go's client did not forward the header to the subdomain (%d): the test proves nothing", a)
	}
	repo := PublicRepository("r", "https://example.com:"+repoPort).WithBasicAuth(repoUser, repoPass)
	if _, err := (&Fetcher{Client: client}).DownloadIndex(context.Background(), repo, filepath.Join(t.TempDir(), "i.yaml")); err != nil {
		t.Fatal(err)
	}
	if n, a := other.sawAuth(); n != 2 || a != 1 {
		t.Errorf("the subdomain saw %d requests, %d with credentials; want Cogpit's without", n, a)
	}
}

// A chart URL that the index points to another host gets no credentials; with
// pass_credentials_all it does.
func TestCredentialsNotSentToAChartElsewhere(t *testing.T) {
	for _, all := range []bool{false, true} {
		home := testkit.NewHelmRepo(t, webChart)
		elsewhere := testkit.NewHelmRepo(t, webChart) // the same archive: charts are packaged reproducibly
		resp, err := home.Client().Get(home.URL + "/index.yaml")
		if err != nil {
			t.Fatal(err)
		}
		index, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		home.Serve("/index.yaml", []byte(strings.ReplaceAll(string(index), home.URL, elsewhere.URL)))
		repo := PublicRepository("home", home.URL).WithBasicAuth(repoUser, repoPass)
		repo.PassCredentialsAll = all
		f := &Fetcher{Client: home.Client()}
		idx, err := f.DownloadIndex(context.Background(), repo, filepath.Join(t.TempDir(), "i.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		web, _ := idx.Chart("web")
		v, _ := web.Latest()
		if !strings.HasPrefix(v.URLs[0], elsewhere.URL) {
			t.Fatalf("the chart's URL %s", v.URLs[0])
		}
		if _, err := f.FetchChart(context.Background(), repo, "web", v); err != nil {
			t.Fatal(err)
		}
		want := 0
		if all {
			want = 1
		}
		if home.Authorized("/index.yaml") != 1 || elsewhere.Requests("/web-0.2.0.tgz") != 1 || elsewhere.Authorized("/web-0.2.0.tgz") != want {
			t.Errorf("all=%v: index authorized %d; chart elsewhere %d requests, %d authorized", all,
				home.Authorized("/index.yaml"), elsewhere.Requests("/web-0.2.0.tgz"), elsewhere.Authorized("/web-0.2.0.tgz"))
		}
	}
}

// A repository served over plain http with a user and password (H-40, owner): nothing is requested
// until the engine hands over a copy the user allowed, so neither the credentials nor a request
// without them leave Cogpit; then it is used as helm uses it, credentials included, its index may
// point to https charts; a user and password in its URL are its basic auth and are shown nowhere;
// an error shows the URL redacted. An http repository without credentials downloads as before (only
// H-41 applies).
func TestPlainHTTPRepository(t *testing.T) {
	src := testkit.NewHelmRepo(t, webChart)
	archive := src.Archive("web-0.2.0.tgz")
	resp, err := src.Client().Get(src.URL + "/index.yaml")
	if err != nil {
		t.Fatal(err)
	}
	index, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	var mu sync.Mutex
	var sawUser string
	requests := 0
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		user, pass, _ := req.BasicAuth()
		mu.Lock()
		sawUser = user
		requests++
		mu.Unlock()
		switch {
		case strings.HasPrefix(req.URL.Path, "/public/"):
			if req.Header.Get("Authorization") != "" {
				http.Error(w, "credentials sent", http.StatusBadRequest)
				return
			}
			_, _ = w.Write([]byte(strings.ReplaceAll(string(index), src.URL+"/", "")))
		case pass != repoPass:
			http.Error(w, "unauthorized", http.StatusUnauthorized)
		case req.URL.Path == "/charts/index.yaml":
			_, _ = w.Write([]byte(strings.ReplaceAll(string(index), src.URL+"/", "")))
		case req.URL.Path == "/charts/web-0.2.0.tgz":
			_, _ = w.Write(archive)
		default:
			http.NotFound(w, req)
		}
	}))
	defer plain.Close()
	withUser := strings.Replace(plain.URL, "http://", "http://"+repoUser+":"+repoPass+"@", 1) + "/charts"
	repo := readEntry(t, fmt.Sprintf("- name: plain\n  url: %s\n", withUser))
	if !repo.PlainHTTP() || !repo.HasBasicAuth() || !repo.CredentialsOverHTTP() || strings.Contains(repo.URL, repoPass) || strings.Contains(fmt.Sprint(repo), repoPass) {
		t.Fatalf("repository %s (%s)", repo, repo.URL)
	}
	f := &Fetcher{}

	// Not allowed: refused before any request.
	_, err = f.DownloadIndex(context.Background(), repo, filepath.Join(t.TempDir(), "i.yaml"))
	if !errors.Is(err, ErrCredentialsOverHTTP) {
		t.Fatalf("an http repository with credentials, not allowed: %v", err)
	}
	noSecrets(t, "not allowed", err)
	if _, err := f.FetchChart(context.Background(), repo, "web", ChartVersion{Version: "0.2.0", URLs: []string{"web-0.2.0.tgz"}}); !errors.Is(err, ErrCredentialsOverHTTP) {
		t.Errorf("a chart of an http repository with credentials, not allowed: %v", err)
	}
	mu.Lock()
	if requests != 0 {
		t.Errorf("the server saw %d requests before the user allowed any", requests)
	}
	mu.Unlock()

	// Allowed: as helm uses it.
	allowed := repo.AllowCredentialsOverHTTP()
	if repo.HTTPBinding() != allowed.HTTPBinding() {
		t.Error("the allowed copy binds something else")
	}
	idx, err := f.DownloadIndex(context.Background(), allowed, filepath.Join(t.TempDir(), "i.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	web, _ := idx.Chart("web")
	v, _ := web.Latest()
	if _, err := f.FetchChart(context.Background(), allowed, "web", v); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	if sawUser != repoUser {
		t.Errorf("the server saw user %q", sawUser)
	}
	mu.Unlock()
	missing := *allowed
	missing.URL = plain.URL + "/nothing?token=tok-53"
	_, err = f.DownloadIndex(context.Background(), &missing, filepath.Join(t.TempDir(), "i.yaml"))
	if err == nil || !strings.Contains(err.Error(), "404") || !strings.Contains(err.Error(), "token=•••") {
		t.Errorf("a missing index: %v", err)
	}
	noSecrets(t, "a missing index", err, "tok-53")

	// Without credentials an http repository needs no allowance.
	public := readEntry(t, fmt.Sprintf("- name: public\n  url: %s/public\n", plain.URL))
	if public.CredentialsOverHTTP() || public.HTTPBinding() != "" {
		t.Fatalf("a public http repository: %v %q", public.CredentialsOverHTTP(), public.HTTPBinding())
	}
	if _, err := f.DownloadIndex(context.Background(), public, filepath.Join(t.TempDir(), "i.yaml")); err != nil {
		t.Errorf("a public http repository: %v", err)
	}
}

// The allowance's binding (H-40): the repository's name, URL and user; a changed password keeps it.
// The authorization transport never adds the credentials to a plain http request it was not
// allowed for, whatever reaches it.
func TestHTTPBinding(t *testing.T) {
	base := PublicRepository("plain", "http://charts.example.com/x").WithBasicAuth(repoUser, repoPass)
	same := PublicRepository("plain", "http://charts.example.com/x").WithBasicAuth(repoUser, "another-password")
	if base.HTTPBinding() == "" || base.HTTPBinding() != same.HTTPBinding() {
		t.Error("a changed password changed the binding")
	}
	for what, r := range map[string]*Repository{
		"name": PublicRepository("other", "http://charts.example.com/x").WithBasicAuth(repoUser, repoPass),
		"URL":  PublicRepository("plain", "http://charts.example.com/y").WithBasicAuth(repoUser, repoPass),
		"user": PublicRepository("plain", "http://charts.example.com/x").WithBasicAuth("bob-chart-user", repoPass),
	} {
		if r.HTTPBinding() == base.HTTPBinding() {
			t.Errorf("a changed %s kept the binding", what)
		}
	}
	if strings.Contains(base.HTTPBinding(), repoPass) || strings.Contains(base.HTTPBinding(), repoUser) {
		t.Error("the binding quotes a credential")
	}

	var sawAuth bool
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		sawAuth = req.Header.Get("Authorization") != ""
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
	})
	for _, plainAllowed := range []bool{false, true} {
		a := &authTransport{base: rt, auth: basicAuth{user: repoUser, pass: repoPass}, origin: origin(base.URL), plain: plainAllowed}
		req, _ := http.NewRequest(http.MethodGet, base.URL+"/index.yaml", nil)
		if _, err := a.RoundTrip(req); err != nil {
			t.Fatal(err)
		}
		if sawAuth != plainAllowed {
			t.Errorf("allowed %v: credentials sent %v", plainAllowed, sawAuth)
		}
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

// clientPair makes a self-signed client certificate and its key, PEM.
func clientPair(t *testing.T, name string) (certPEM, keyPEM []byte, cert *x509.Certificate) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: name},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}, BasicConstraintsValid: true}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, _ = x509.ParseCertificate(der)
	kder, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kder}), cert
}

// A repository that wants a client certificate gets the one Helm's entry names, read at every
// download (a rotated file applies); a key that does not fit is refused naming the files, never
// their content.
func TestClientCertificateRepository(t *testing.T) {
	certPEM, keyPEM, cert := clientPair(t, "cogpit-test-client")
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, emptyIndex) }))
	srv.TLS = &tls.Config{ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: pool, MinVersion: tls.VersionTLS12}
	srv.StartTLS()
	defer srv.Close()
	dir := t.TempDir()
	certFile, keyFile := filepath.Join(dir, "client.pem"), filepath.Join(dir, "client-key.pem")
	writeFile(t, certFile, string(certPEM))
	writeFile(t, keyFile, string(keyPEM))
	ca := caFile(t, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw}))
	repo := readEntry(t, fmt.Sprintf("- name: certs\n  url: %s\n  caFile: %s\n  certFile: %s\n  keyFile: %s\n", srv.URL, ca, certFile, keyFile))
	f := &Fetcher{}
	dest := filepath.Join(t.TempDir(), "i.yaml")
	if _, err := f.DownloadIndex(context.Background(), repo, dest); err != nil {
		t.Fatalf("with the client certificate: %v", err)
	}
	without := readEntry(t, fmt.Sprintf("- name: certs\n  url: %s\n  caFile: %s\n", srv.URL, ca))
	if _, err := f.DownloadIndex(context.Background(), without, dest); err == nil {
		t.Error("downloaded without a client certificate")
	}
	// Rotated to a certificate the server does not trust: the next download takes it.
	otherCert, otherKey, _ := clientPair(t, "someone-else")
	writeFile(t, certFile, string(otherCert))
	writeFile(t, keyFile, string(otherKey))
	if _, err := f.DownloadIndex(context.Background(), repo, dest); err == nil {
		t.Error("the rotated, untrusted certificate was not used")
	}
	writeFile(t, keyFile, "-----BEGIN EC PRIVATE KEY-----\nPRIVATE-KEY-SECRET-61\n-----END EC PRIVATE KEY-----\n")
	_, err := f.DownloadIndex(context.Background(), repo, dest)
	if err == nil || !strings.Contains(err.Error(), "client-key.pem") || strings.Contains(err.Error(), "PRIVATE-KEY-SECRET-61") {
		t.Errorf("a key that does not fit: %v", err)
	}
}

// insecure_skip_tls_verify switches the certificate's check off for that repository only.
func TestInsecureSkipTLSVerify(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, emptyIndex) }))
	defer srv.Close()
	dest := filepath.Join(t.TempDir(), "i.yaml")
	if _, err := (&Fetcher{}).DownloadIndex(context.Background(), readEntry(t, "- name: x\n  url: "+srv.URL+"\n"), dest); err == nil {
		t.Error("an untrusted certificate was accepted")
	}
	insecure := readEntry(t, "- name: x\n  url: "+srv.URL+"\n  insecure_skip_tls_verify: true\n")
	if _, err := (&Fetcher{}).DownloadIndex(context.Background(), insecure, dest); err != nil {
		t.Errorf("insecure_skip_tls_verify: %v", err)
	}
}

// The guard: whatever an error says, the repository's password and user leave it as •••, and
// errors.Is still finds what it wraps.
func TestScrubbedErrors(t *testing.T) {
	repo := PublicRepository("r", "https://charts.example.com").WithBasicAuth(repoUser, repoPass)
	err := repo.scrubError(fmt.Errorf("odd: %s:%s: %w", repoUser, repoPass, ErrDigestMismatch))
	noSecrets(t, "scrubbed", err)
	if !errors.Is(err, ErrDigestMismatch) || !strings.Contains(err.Error(), "•••") {
		t.Errorf("scrubbed: %v", err)
	}
	if repo.scrubError(nil) != nil {
		t.Error("nil became an error")
	}
}
