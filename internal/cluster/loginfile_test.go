package cluster

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/testkit"
)

// TestLoginInFileSeesARenewedLogin is F28's cause: a connection keeps the login it read at its
// connect, and LoginInFile tells whether the kubeconfig file holds another one now: a renewed
// token or client certificate, another server. The same file written again, or one whose other
// contexts changed, is the same login.
func TestLoginInFileSeesARenewedLogin(t *testing.T) {
	srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{Token: "first"})
	defer srv.Close()
	file := filepath.Join(t.TempDir(), "ske.yaml")
	write := func(data []byte) {
		t.Helper()
		if err := os.WriteFile(file, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(srv.KubeconfigWithToken("mgmt", "", "first"))
	c, err := Connect(context.Background(), Options{KubeconfigPaths: []string{file}, Context: "mgmt"})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if c.CredentialPlugin() {
		t.Error("a token's context runs no credential plugin")
	}
	fp, changed, err := c.LoginInFile()
	if err != nil || changed || fp == "" {
		t.Fatalf("the file as connected: %q %v %v", fp, changed, err)
	}
	write(srv.KubeconfigWithToken("mgmt", "", "first")) // written again, the same login
	if _, changed, err := c.LoginInFile(); err != nil || changed {
		t.Errorf("the same file written again: %v %v", changed, err)
	}
	write(srv.KubeconfigWithToken("mgmt", "kube-system", "first")) // another default namespace: not the login
	if _, changed, err := c.LoginInFile(); err != nil || changed {
		t.Errorf("another namespace: %v %v", changed, err)
	}
	write(srv.KubeconfigWithToken("mgmt", "", "second"))
	fp2, changed, err := c.LoginInFile()
	if err != nil || !changed || fp2 == fp {
		t.Errorf("a renewed token: %v %v", changed, err)
	}
	ca := testkit.NewCert(t, testkit.CertSpec{CommonName: "ca", CA: true})
	cert := testkit.NewCert(t, testkit.CertSpec{CommonName: "admin", Parent: ca})
	write(srv.KubeconfigWithClientCert("mgmt", cert.PEM(), cert.KeyPEM(t)))
	if _, changed, err := c.LoginInFile(); err != nil || !changed {
		t.Errorf("a client certificate: %v %v", changed, err)
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if _, changed, err := c.LoginInFile(); err == nil || changed {
		t.Errorf("the file gone: %v %v", changed, err)
	}
	var bare Conn
	if _, _, err := bare.LoginInFile(); !errors.Is(err, errNoLoginFile) {
		t.Errorf("a connection made from no file: %v", err)
	}
}

// TestLoginFingerprintNamesNoCredential: the fingerprint is an HMAC under the process's own key,
// not the credential or a plain hash of it.
func TestLoginFingerprintNamesNoCredential(t *testing.T) {
	srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{Token: "s3cret-token"})
	defer srv.Close()
	file := filepath.Join(t.TempDir(), "k.yaml")
	if err := os.WriteFile(file, srv.KubeconfigWithToken("k", "", "s3cret-token"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Connect(context.Background(), Options{KubeconfigPaths: []string{file}, Context: "k"})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	fp, _, err := c.LoginInFile()
	if err != nil || len(fp) != 64 || fp != c.login {
		t.Fatalf("fingerprint %q %v", fp, err)
	}
	for _, s := range []string{"s3cret-token", srv.URL} {
		if strings.Contains(fp, s) {
			t.Errorf("the fingerprint holds %q", s)
		}
	}
}

// TestCredentialPluginIsNoted: a context whose user runs a credential plugin says so; its login in
// the file is the plugin's configuration, unchanged while the plugin hands out new tokens.
func TestCredentialPluginIsNoted(t *testing.T) {
	srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{Token: "tok"})
	defer srv.Close()
	p := testkit.NewLoginPlugin(t, srv, "oidc")
	p.Give("tok", time.Time{})
	c, err := Connect(context.Background(), Options{KubeconfigPaths: []string{p.Kubeconfig}, Context: "oidc"})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if !c.CredentialPlugin() {
		t.Error("the plugin's context does not say it runs one")
	}
	p.Give("another", time.Time{})
	if _, changed, err := c.LoginInFile(); err != nil || changed {
		t.Errorf("the plugin's file after a new token: %v %v", changed, err)
	}
}
