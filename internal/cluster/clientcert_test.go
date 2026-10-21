package cluster

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/kubeconfig"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// fileCertKubeconfig is a kubeconfig for srv whose one context, "certs", logs in with a client
// certificate file and a key file, as written (relative paths resolve against the kubeconfig's
// folder).
func fileCertKubeconfig(srv *testkit.FakeAPIServer, certPath, keyPath string) string {
	return `apiVersion: v1
kind: Config
clusters:
- name: fake
  cluster:
    server: ` + srv.URL + `
    certificate-authority-data: ` + base64.StdEncoding.EncodeToString(srv.CAData()) + `
users:
- name: u
  user:
    client-certificate: ` + certPath + `
    client-key: ` + keyPath + `
contexts:
- name: certs
  context: {cluster: fake, user: u}
`
}

// writeCert writes a client certificate valid from notBefore to notAfter and its key into dir,
// and returns their paths.
func writeCert(t *testing.T, dir string, notBefore, notAfter time.Time) (certPath, keyPath string, cert *testkit.TestCert) {
	t.Helper()
	cert = testkit.NewCert(t, testkit.CertSpec{CommonName: "u", NotBefore: notBefore, NotAfter: notAfter})
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	certPath, keyPath = filepath.Join(dir, "client.crt"), filepath.Join(dir, "client.key")
	if err := os.WriteFile(certPath, cert.PEM(), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, cert.KeyPEM(t), 0o600); err != nil {
		t.Fatal(err)
	}
	return certPath, keyPath, cert
}

// inspectedCert is the client certificate InspectFile reports for the context "certs" of path.
func inspectedCert(t *testing.T, path string) *kubeconfig.CertValidity {
	t.Helper()
	in, err := InspectFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range in.Contexts {
		if c.Name == "certs" {
			return c.ClientCert
		}
	}
	t.Fatal("no context certs")
	return nil
}

// resetCertMemo forgets what earlier tests' connects read, and puts macPrivacy back afterwards.
func resetCertMemo(t *testing.T, privacy bool) {
	t.Helper()
	old := macPrivacy.Swap(privacy)
	certMemo.Lock()
	certMemo.m = map[string]kubeconfig.CertValidity{}
	certMemo.Unlock()
	t.Cleanup(func() { macPrivacy.Store(old) })
}

// realTempDir is t.TempDir with its links resolved (macOS's /var is a link to /private/var), so
// that a home folder made in it is named as macOS names it.
func realTempDir(t *testing.T) string {
	t.Helper()
	d, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// A client certificate file beside the kubeconfig (a relative path, as minikube's and a hand-made
// kubeconfig's are) is read when the contexts are listed, by InspectFile and by ListContexts, and
// gives the certificate's dates (D-75).
func TestClientCertificateFileIsRead(t *testing.T) {
	resetCertMemo(t, false)
	srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{})
	defer srv.Close()
	dir := realTempDir(t)
	now := time.Now().UTC().Truncate(time.Second)
	_, _, cert := writeCert(t, filepath.Join(dir, "pki"), now.Add(-time.Hour), now.Add(40*24*time.Hour))
	kc := filepath.Join(dir, "config")
	if err := os.WriteFile(kc, []byte(fileCertKubeconfig(srv, "pki/client.crt", "pki/client.key")), 0o600); err != nil {
		t.Fatal(err)
	}
	if v := inspectedCert(t, kc); v == nil || !v.NotAfter.Equal(cert.Cert.NotAfter) {
		t.Errorf("InspectFile: %+v, want until %v", v, cert.Cert.NotAfter)
	}
	list, err := ListContexts([]string{kc})
	if err != nil || len(list) != 1 || list[0].ClientCert == nil || !list[0].ClientCert.NotAfter.Equal(cert.Cert.NotAfter) {
		t.Errorf("ListContexts: %+v, %v", list, err)
	}
}

// On macOS, a client certificate file in a place macOS protects (the Documents folder here, in a
// home folder of the test's own), or one a link leads into such a place, is not read while the
// contexts are listed: no privacy prompt the user did not ask for (D-72). Its dates show after a
// connect, which read it as client-go did; a certificate renewed on disk shows at the next
// connect. A file outside those places is read at every listing. Connecting with a certificate
// that expired is not refused by Cogpit: the connect reaches the server (this one does not check
// client certificates).
func TestProtectedClientCertificateIsReadAtConnect(t *testing.T) {
	resetCertMemo(t, true)
	home := realTempDir(t)
	t.Setenv("HOME", home)
	if runtime.GOOS == "windows" {
		t.Skip("links and HOME as macOS has them")
	}
	srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{})
	defer srv.Close()
	now := time.Now().UTC().Truncate(time.Second)
	certPath, keyPath, expired := writeCert(t, filepath.Join(home, "Documents", "k8s"), now.Add(-400*24*time.Hour), now.Add(-3*24*time.Hour))
	kube := filepath.Join(home, ".kube")
	if err := os.MkdirAll(kube, 0o700); err != nil {
		t.Fatal(err)
	}
	// The same certificate three ways: in Documents, through a link to the file, through a link to
	// its folder.
	if err := os.Symlink(certPath, filepath.Join(kube, "linked.crt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Dir(certPath), filepath.Join(home, "certs")); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{certPath, filepath.Join(kube, "linked.crt"), filepath.Join(home, "certs", "client.crt"),
		filepath.Join(home, "documents", "k8s", "client.crt")} {
		if readableUnasked(p) {
			t.Errorf("%s counts as readable without asking", p)
		}
	}
	kc := filepath.Join(kube, "config")
	if err := os.WriteFile(kc, []byte(fileCertKubeconfig(srv, certPath, keyPath)), 0o600); err != nil {
		t.Fatal(err)
	}
	if v := inspectedCert(t, kc); v != nil {
		t.Fatalf("listed before a connect: %+v", v)
	}

	c, err := Connect(context.Background(), Options{KubeconfigPaths: []string{kc}, Context: "certs"})
	if err != nil {
		t.Fatalf("a connect with an expired certificate is Cogpit's to try, not to refuse: %v", err)
	}
	c.Close()
	if srv.Requests()["discovery"] == 0 {
		t.Errorf("the connect reached no server: %v", srv.Requests())
	}
	v := inspectedCert(t, kc)
	if v == nil || !v.NotAfter.Equal(expired.Cert.NotAfter) || v.StateAt(now) != kubeconfig.CertExpired {
		t.Fatalf("after the connect: %+v, want until %v", v, expired.Cert.NotAfter)
	}

	// Renewed on disk: still the old date until the next connect.
	_, _, renewed := writeCert(t, filepath.Dir(certPath), now.Add(-time.Hour), now.Add(365*24*time.Hour))
	if v := inspectedCert(t, kc); v == nil || !v.NotAfter.Equal(expired.Cert.NotAfter) {
		t.Errorf("read again at a listing: %+v", v)
	}
	c, err = Connect(context.Background(), Options{KubeconfigPaths: []string{kc}, Context: "certs"})
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
	if v := inspectedCert(t, kc); v == nil || !v.NotAfter.Equal(renewed.Cert.NotAfter) {
		t.Errorf("after the next connect: %+v, want until %v", v, renewed.Cert.NotAfter)
	}

	// Outside the protected places: read at the listing.
	outside, _, valid := writeCert(t, filepath.Join(home, ".minikube", "profiles", "m"), now.Add(-time.Hour), now.Add(30*24*time.Hour))
	if !readableUnasked(outside) {
		t.Errorf("%s does not count as readable", outside)
	}
	if err := os.WriteFile(kc, []byte(fileCertKubeconfig(srv, outside, keyPath)), 0o600); err != nil {
		t.Fatal(err)
	}
	if v := inspectedCert(t, kc); v == nil || !v.NotAfter.Equal(valid.Cert.NotAfter) {
		t.Errorf("outside: %+v, want until %v", v, valid.Cert.NotAfter)
	}
}

// The places counted as protected: the Desktop, Documents and Downloads folders, iCloud Drive,
// file providers' folders, other apps' containers and /Volumes, in any case; the home folder
// itself, ~/.kube and ~/.minikube are not.
func TestProtectedPlaces(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("macOS's places as Unix paths; off macOS readableUnasked never asks protectedPlace")
	}
	homes := []string{"/Users/u"}
	for p, want := range map[string]bool{
		"/Users/u/Desktop/a.crt": true, "/Users/u/Documents": true, "/Users/u/DOWNLOADS/x/y.crt": true,
		"/Users/u/Library/Mobile Documents/com~apple~CloudDocs/c.crt": true, "/Users/u/Library/CloudStorage/Dropbox/c.crt": true,
		"/Users/u/Library/Containers/x/c.crt": true, "/Users/u/Library/Group Containers/x/c.crt": true,
		"/Volumes/USB/c.crt": true, "/Volumes": true,
		"/Users/u": false, "/Users/u/.kube/c.crt": false, "/Users/u/.minikube/profiles/m/client.crt": false,
		"/Users/u/Documentsx/c.crt": false, "/Users/u/Library/Preferences/c.crt": false, "/etc/kubernetes/admin.crt": false,
	} {
		if got := protectedPlace(homes, p); got != want {
			t.Errorf("%s: protected %v, want %v", p, got, want)
		}
	}
}

// A file that cannot be read gives no date and is logged once at debug level; a connect that read
// a file that is gone since forgets it.
func TestUnreadableClientCertificateFile(t *testing.T) {
	resetCertMemo(t, false)
	dir := realTempDir(t)
	missing := filepath.Join(dir, "gone.crt")
	if v, ok, err := clientCertOfFile(missing); ok || err == nil || v != (kubeconfig.CertValidity{}) {
		t.Errorf("a missing file: %+v %v %v", v, ok, err)
	}
	garbage := filepath.Join(dir, "garbage.crt")
	if err := os.WriteFile(garbage, []byte("not a certificate"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := clientCertOfFile(garbage); ok || err == nil {
		t.Errorf("garbage: %v %v", ok, err)
	}
	if _, ok, err := clientCertOfFile("relative.crt"); ok || err != nil {
		t.Errorf("a relative path is not read: %v %v", ok, err)
	}
	now := time.Now()
	p, _, _ := writeCert(t, filepath.Join(dir, "memo"), now.Add(-time.Hour), now.Add(time.Hour))
	noteClientCertFile(p)
	certMemo.Lock()
	_, kept := certMemo.m[p]
	certMemo.Unlock()
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	noteClientCertFile(p)
	certMemo.Lock()
	_, still := certMemo.m[p]
	certMemo.Unlock()
	if !kept || still {
		t.Errorf("memo: kept %v, still after the file went %v", kept, still)
	}
}
