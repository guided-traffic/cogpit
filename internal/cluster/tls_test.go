package cluster

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	"github.com/guided-traffic/cogpit/internal/testkit"
)

// The kubeconfig's TLS and proxy fields (platform parity: "Custom CA, tls-server-name,
// insecure-skip-tls-verify" and "proxy-url in the kubeconfig"), through the real cluster layer:
// clientcmd builds the rest.Config from the kubeconfig and Connect proves the server answers.
// Each server is the fake API server presenting a certificate of a test CA, and requires the
// kubeconfig's token, so a connect that succeeds also proves where the token went.

// tlsCluster is the cluster entry of a test kubeconfig: the fields under clusters[].cluster.
type tlsCluster struct {
	server     string
	caData     []byte // certificate-authority-data (PEM; base64-encoded into the YAML)
	caFile     string // certificate-authority
	serverName string // tls-server-name
	insecure   bool   // insecure-skip-tls-verify
	proxyURL   string // proxy-url, written in single quotes
}

// writeTLSKubeconfig writes a kubeconfig with one context "tls" whose cluster has the given fields
// and whose user has secretToken.
func writeTLSKubeconfig(t *testing.T, c tlsCluster) string {
	t.Helper()
	var b strings.Builder
	b.WriteString("apiVersion: v1\nkind: Config\ncurrent-context: tls\nclusters:\n- name: c\n  cluster:\n")
	b.WriteString("    server: " + c.server + "\n")
	if c.caData != nil {
		b.WriteString("    certificate-authority-data: " + base64.StdEncoding.EncodeToString(c.caData) + "\n")
	}
	if c.caFile != "" {
		b.WriteString("    certificate-authority: " + c.caFile + "\n")
	}
	if c.serverName != "" {
		b.WriteString("    tls-server-name: " + c.serverName + "\n")
	}
	if c.insecure {
		b.WriteString("    insecure-skip-tls-verify: true\n")
	}
	if c.proxyURL != "" {
		b.WriteString("    proxy-url: '" + c.proxyURL + "'\n")
	}
	b.WriteString("users:\n- name: u\n  user:\n    token: " + secretToken + "\n")
	b.WriteString("contexts:\n- name: tls\n  context:\n    cluster: c\n    user: u\n")
	return writeFile(t, "kubeconfig", b.String())
}

// connectTLS connects the kubeconfig's context.
func connectTLS(t *testing.T, path string) (*Conn, error) {
	t.Helper()
	c, err := Connect(context.Background(), Options{KubeconfigPaths: []string{path}})
	if c != nil {
		t.Cleanup(c.Close)
	}
	return c, err
}

// mustNotLeak fails when err's text holds the kubeconfig's token or one of the secrets.
func mustNotLeak(t *testing.T, err error, secrets ...string) {
	t.Helper()
	for _, s := range append(secrets, secretToken) {
		if err != nil && strings.Contains(err.Error(), s) {
			t.Errorf("the error quotes a secret: %v", err)
		}
	}
}

// certificate-authority-data with the CA the server's certificate chains to connects, and so does
// the CA as a file; without it, client-go trusts the system's roots only, which do not include
// the test CA: the connect fails with a sentence about the certificate, never the token.
func TestConnectTrustsTheKubeconfigCA(t *testing.T) {
	ca := testkit.NewTestCA(t, "cogpit test CA")
	srv := testkit.NewFakeAPIServerWithCert(testkit.FakeAPIOptions{Token: secretToken}, ca.Issue(t, "127.0.0.1"))
	defer srv.Close()

	c, err := connectTLS(t, writeTLSKubeconfig(t, tlsCluster{server: srv.URL, caData: ca.PEM()}))
	if err != nil {
		t.Fatalf("Connect with the CA's data: %v", err)
	}
	if _, err := c.Kube().CoreV1().Pods("").List(context.Background(), metav1.ListOptions{}); err != nil {
		t.Fatalf("a list over the verified connection: %v", err)
	}
	if c.Insecure() {
		t.Error("a verified connection reports certificate checks off")
	}

	caFile := filepath.Join(t.TempDir(), "ca.crt")
	if err := os.WriteFile(caFile, ca.PEM(), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := connectTLS(t, writeTLSKubeconfig(t, tlsCluster{server: srv.URL, caFile: caFile})); err != nil {
		t.Fatalf("Connect with the CA as a file: %v", err)
	}

	_, err = connectTLS(t, writeTLSKubeconfig(t, tlsCluster{server: srv.URL}))
	var ce *ConnectError
	if !errors.As(err, &ce) {
		t.Fatalf("err = %v, want a ConnectError", err)
	}
	t.Logf("without the CA: %v", err)
	if !strings.Contains(err.Error(), srv.URL) || !strings.Contains(err.Error(), "certificate") {
		t.Errorf("the error does not say the server's certificate is the problem: %v", err)
	}
	mustNotLeak(t, err)

	// Another CA's data does not help either.
	other := testkit.NewTestCA(t, "another CA")
	_, err = connectTLS(t, writeTLSKubeconfig(t, tlsCluster{server: srv.URL, caData: other.PEM()}))
	if err == nil {
		t.Fatal("connected with another CA's data")
	}
	t.Logf("with another CA: %v", err)
	mustNotLeak(t, err)
}

// A certificate that names another host than the server URL's verifies only with tls-server-name
// set to a name it holds: the API server reached by address (a tunnel, a load balancer's IP), its
// certificate issued for its DNS name.
func TestConnectWithTLSServerName(t *testing.T) {
	ca := testkit.NewTestCA(t, "cogpit test CA")
	srv := testkit.NewFakeAPIServerWithCert(testkit.FakeAPIOptions{Token: secretToken}, ca.Issue(t, "api.cogpit.test"))
	defer srv.Close()

	_, err := connectTLS(t, writeTLSKubeconfig(t, tlsCluster{server: srv.URL, caData: ca.PEM()}))
	if err == nil {
		t.Fatal("connected although the certificate names another host")
	}
	t.Logf("without tls-server-name: %v", err)
	if !strings.Contains(err.Error(), "tls-server-name") {
		t.Errorf("the error does not point at tls-server-name: %v", err)
	}
	mustNotLeak(t, err)

	if _, err := connectTLS(t, writeTLSKubeconfig(t, tlsCluster{server: srv.URL, caData: ca.PEM(), serverName: "api.cogpit.test"})); err != nil {
		t.Fatalf("Connect with tls-server-name: %v", err)
	}
	_, err = connectTLS(t, writeTLSKubeconfig(t, tlsCluster{server: srv.URL, caData: ca.PEM(), serverName: "other.cogpit.test"}))
	if err == nil {
		t.Fatal("connected with a tls-server-name the certificate does not hold")
	}
	mustNotLeak(t, err)
}

// insecure-skip-tls-verify connects to a server whose certificate nobody trusts, and the token
// goes to it: the server here requires the token, so the connect proves it arrived. The
// connection says so (Conn.Insecure), for the warnings the engine and the window show. Together
// with the kubeconfig's own CA it is refused by client-go, with a sentence that says why.
func TestInsecureSkipTLSVerifyConnectsToAnUntrustedServer(t *testing.T) {
	untrusted := testkit.NewTestCA(t, "nobody's CA")
	srv := testkit.NewFakeAPIServerWithCert(testkit.FakeAPIOptions{Token: secretToken}, untrusted.Issue(t, "elsewhere.test"))
	defer srv.Close()

	if _, err := connectTLS(t, writeTLSKubeconfig(t, tlsCluster{server: srv.URL})); err == nil {
		t.Fatal("connected to an untrusted certificate without insecure-skip-tls-verify")
	}
	c, err := connectTLS(t, writeTLSKubeconfig(t, tlsCluster{server: srv.URL, insecure: true}))
	if err != nil {
		t.Fatalf("Connect with insecure-skip-tls-verify: %v", err)
	}
	if !c.Insecure() {
		t.Error("the connection does not report certificate checks off")
	}
	if !c.RESTConfig().Insecure {
		t.Error("a copy of the configuration lost the flag")
	}
	if (&Conn{}).Insecure() || (*Conn)(nil).Insecure() {
		t.Error("a zero or nil connection reports certificate checks off")
	}
	if srv.Requests()["discovery"] < 1 {
		t.Errorf("the server saw %v", srv.Requests())
	}

	ca := testkit.NewTestCA(t, "cogpit test CA")
	_, err = connectTLS(t, writeTLSKubeconfig(t, tlsCluster{server: srv.URL, insecure: true, caData: ca.PEM()}))
	if err == nil {
		t.Fatal("connected with insecure-skip-tls-verify and a CA together")
	}
	t.Logf("insecure and a CA: %v", err)
	if !strings.Contains(err.Error(), "insecure") {
		t.Errorf("the error does not name the conflict: %v", err)
	}
	mustNotLeak(t, err)
}

// clientcmd attaches a kubeconfig's credentials only for an https server (rest.IsConfigTransportTLS):
// a plain-http server gets no token, so neither it nor a proxy on the way reads one.
func TestPlainHTTPServerGetsNoCredential(t *testing.T) {
	var mu sync.Mutex
	var auths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		auths = append(auths, r.Header.Get("Authorization"))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"major":"1","minor":"37","gitVersion":"v1.37.1-plain"}`))
	}))
	defer srv.Close()

	c, err := connectTLS(t, writeTLSKubeconfig(t, tlsCluster{server: srv.URL}))
	if err != nil {
		t.Fatalf("Connect over http: %v", err)
	}
	if c.ServerVersion() != "v1.37.1-plain" {
		t.Errorf("version %q", c.ServerVersion())
	}
	mu.Lock()
	defer mu.Unlock()
	if len(auths) == 0 {
		t.Fatal("the server saw no request")
	}
	for _, a := range auths {
		if a != "" {
			t.Errorf("a plain-http server was sent an Authorization header")
		}
	}
}

// The kubeconfig's proxy-url carries every request of the connection, its user and password going
// to the proxy; the proxy of the cluster's settings in Cogpit (Options.Proxy) replaces it.
func TestKubeconfigProxyURLIsHonoured(t *testing.T) {
	srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{Token: secretToken})
	defer srv.Close()
	server, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	kubeProxy := testkit.NewForwardProxy(t, "bob", proxyPassword)
	path := writeTLSKubeconfig(t, tlsCluster{server: srv.URL, caData: srv.CAData(),
		proxyURL: kubeProxy.URLWithUser("bob", proxyPassword).String()})

	c, err := connectTLS(t, path)
	if err != nil {
		t.Fatalf("Connect through the kubeconfig's proxy-url: %v", err)
	}
	if n := kubeProxy.Connects()[server.Host]; n < 1 {
		t.Fatalf("the kubeconfig's proxy tunnelled %v, want the API server %s", kubeProxy.Connects(), server.Host)
	}
	if !slices.Contains(kubeProxy.Users(), "bob") {
		t.Errorf("the proxy saw the users %q, want bob", kubeProxy.Users())
	}
	if _, err := c.Kube().CoreV1().Pods("").List(context.Background(), metav1.ListOptions{}); err != nil {
		t.Fatalf("a list through the proxy: %v", err)
	}

	// Cogpit's cluster proxy wins: the kubeconfig's sees nothing more.
	before := kubeProxy.Total()
	ours := testkit.NewForwardProxy(t, "", "")
	c2, err := Connect(context.Background(), Options{KubeconfigPaths: []string{path}, Proxy: ours.URL})
	if err != nil {
		t.Fatalf("Connect through the cluster's proxy: %v", err)
	}
	defer c2.Close()
	if _, err := c2.Kube().CoreV1().Pods("").List(context.Background(), metav1.ListOptions{}); err != nil {
		t.Fatalf("a list through the cluster's proxy: %v", err)
	}
	if ours.Connects()[server.Host] < 1 {
		t.Errorf("the cluster's proxy tunnelled %v", ours.Connects())
	}
	if kubeProxy.Total() != before {
		t.Errorf("the kubeconfig's proxy-url still carried %d requests", kubeProxy.Total()-before)
	}
}

// A kubeconfig's proxy-url that client-go cannot use is quoted whole in client-go's errors
// ("invalid 'proxy-url' ..."): its user information must not reach Cogpit's connect error.
func TestKubeconfigProxyPasswordNeverInErrors(t *testing.T) {
	srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{Token: secretToken})
	defer srv.Close()
	for _, tt := range []struct{ name, proxy, secret string }{
		{"unsupported scheme", "ftp://carol:" + proxyPassword + "@127.0.0.1:1", proxyPassword},
		{"unparseable", "http://carol:pw%zz" + proxyPassword + "@127.0.0.1:1", proxyPassword},
		{"refused", "http://carol:" + proxyPassword + "@" + testkit.RefusedAddr, proxyPassword},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := connectTLS(t, writeTLSKubeconfig(t, tlsCluster{server: srv.URL, caData: srv.CAData(), proxyURL: tt.proxy}))
			if err == nil {
				t.Fatal("connected through a proxy that does not exist")
			}
			t.Logf("%s: %v", tt.name, err)
			mustNotLeak(t, err, tt.secret, "carol:")
		})
	}
}

// The user information of a kubeconfig's proxy-url is scrubbed as written, as %q writes it, and its
// password alone (also unescaped); a short password is replaced only within the user information.
func TestKubeconfigProxySecrets(t *testing.T) {
	cfg := clientcmdapi.NewConfig()
	cfg.Clusters["a"] = &clientcmdapi.Cluster{ProxyURL: `http://dave:se"cr%20et@proxy:3128`}
	cfg.Clusters["b"] = &clientcmdapi.Cluster{ProxyURL: "http://proxy:3128"}
	cfg.Clusters["c"] = &clientcmdapi.Cluster{ProxyURL: "socks5://erin:pw@proxy:1080"}
	cfg.Clusters["d"] = nil
	secrets := kubeconfigProxySecrets(*cfg)
	for _, text := range []string{
		`invalid 'proxy-url' "http://dave:se\"cr%20et@proxy:3128"`,
		`could not parse: http://dave:se"cr%20et@proxy:3128`,
		`the password se"cr et`,
		`invalid 'proxy-url' "socks5://erin:pw@proxy:1080"`,
	} {
		got := scrubSecrets(&ConnectError{Context: "x", Problem: text}, secrets).Error()
		for _, leak := range []string{"se\"cr", `se\"cr`, "cr%20et", "cr et", "erin:pw"} {
			if strings.Contains(got, leak) {
				t.Errorf("%q scrubbed to %q, which still holds %q", text, got, leak)
			}
		}
		if !strings.Contains(got, "•••") {
			t.Errorf("%q scrubbed to %q", text, got)
		}
	}
	plain := errors.New("dial tcp: connection refused")
	if scrubSecrets(plain, secrets) != plain {
		t.Error("an error without a secret changed")
	}
	if got := scrubSecrets(errors.New("dave:se\"cr%20et"), secrets); got.Error() != "•••" {
		t.Errorf("plain error scrubbed to %q", got)
	}
}
