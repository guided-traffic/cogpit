package cluster

import (
	"context"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"net"
	"net/url"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/testkit"
)

// The connection log's steps (D-66): what Connect reports through Options.Trace, from the
// configuration it builds the client from, and the error picture of a failed first answer.

// stepLog collects a connect's steps.
type stepLog struct {
	mu    sync.Mutex
	steps []ConnectStep
}

func (l *stepLog) add(st ConnectStep) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.steps = append(l.steps, st)
}

// text is every step's text, one per line, its name in front.
func (l *stepLog) text() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var b strings.Builder
	for _, st := range l.steps {
		b.WriteString(st.Name + ": " + st.Text + "\n")
	}
	return b.String()
}

// find is the first step named name.
func (l *stepLog) find(name string) (ConnectStep, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, st := range l.steps {
		if st.Name == name {
			return st, true
		}
	}
	return ConnectStep{}, false
}

func traceConnect(t *testing.T, path string) (*stepLog, *Conn, error) {
	t.Helper()
	log := &stepLog{}
	c, err := Connect(context.Background(), Options{KubeconfigPaths: []string{path}, Trace: log.add, ConnectTimeout: 5 * time.Second})
	if c != nil {
		t.Cleanup(c.Close)
	}
	return log, c, err
}

// A connect that works reports the kubeconfig, the server, the login without the token, the
// certificate check against the kubeconfig's CA, the proxy and the first answer with its version.
func TestConnectTraceRecordsTheSteps(t *testing.T) {
	ca := testkit.NewTestCA(t, "cogpit trace CA")
	srv := testkit.NewFakeAPIServerWithCert(testkit.FakeAPIOptions{Token: secretToken}, ca.Issue(t, "127.0.0.1"))
	defer srv.Close()
	path := writeTLSKubeconfig(t, tlsCluster{server: srv.URL + "/?debug=x", caData: ca.PEM()})
	log, _, err := traceConnect(t, path)
	if err != nil {
		t.Fatal(err)
	}
	text := log.text()
	for _, want := range []string{
		`kubeconfig: Kubeconfig: context "tls" (cluster "c", user "u") in ` + path,
		"server: API server: " + srv.URL + "/\n",
		"login: Login: bearer token from the kubeconfig (not shown)",
		"tls: TLS: the server's certificate is checked against the kubeconfig's certificate-authority-data (CN=cogpit trace CA",
		"proxy: Proxy: ",
		"answer: First answer: GET /version: Kubernetes v",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the steps lack %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, secretToken) || strings.Contains(text, "debug=x") {
		t.Errorf("a step quotes the token or the URL's query:\n%s", text)
	}
	if st, _ := log.find(StepAnswer); st.Failed {
		t.Errorf("the answer failed: %+v", st)
	}
}

// A client certificate is named by its subject and expiry, never its key.
func TestConnectTraceNamesTheClientCertificate(t *testing.T) {
	cert := testkit.NewCert(t, testkit.CertSpec{CommonName: "alice", NotBefore: time.Now().Add(-time.Hour),
		NotAfter: time.Date(2031, 2, 3, 0, 0, 0, 0, time.UTC), ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}})
	key := cert.KeyPEM(t)
	kc := `apiVersion: v1
kind: Config
current-context: cert
clusters:
- name: c
  cluster: {server: "https://127.0.0.1:1"}
users:
- name: u
  user:
    client-certificate-data: ` + base64.StdEncoding.EncodeToString(cert.PEM()) + `
    client-key-data: ` + base64.StdEncoding.EncodeToString(key) + `
contexts:
- name: cert
  context: {cluster: c, user: u}
`
	log, _, err := traceConnect(t, writeFile(t, "kubeconfig", kc))
	if err == nil {
		t.Fatal("connected to a closed port")
	}
	text := log.text()
	if !strings.Contains(text, "Login: client certificate CN=alice, valid until 2031-02-03") {
		t.Errorf("the login step:\n%s", text)
	}
	if strings.Contains(text, "PRIVATE KEY") || strings.Contains(text, base64.StdEncoding.EncodeToString(key)[:40]) {
		t.Errorf("a step quotes the key:\n%s", text)
	}
}

// A refused port, an unknown certificate authority and a rejected token each fail the first answer
// with their error picture: the class, the presented certificate's public facts, the HTTP status.
func TestConnectTraceErrorPicture(t *testing.T) {
	t.Run("refused", func(t *testing.T) {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		addr := l.Addr().String()
		_ = l.Close()
		log, _, err := traceConnect(t, writeFile(t, "kubeconfig", kubeconfigFor("x", "https://"+addr, "")))
		if err == nil {
			t.Fatal("connected")
		}
		text := log.text()
		if st, ok := log.find(StepAnswer); !ok || !st.Failed || !strings.Contains(st.Text, "the limit is 5s") {
			t.Errorf("the answer step %+v:\n%s", st, text)
		}
		if !strings.Contains(text, "error: Class: TCP: the connection was refused by "+addr) || !strings.Contains(text, "error: Error chain: ") {
			t.Errorf("the picture:\n%s", text)
		}
	})
	t.Run("unknown authority", func(t *testing.T) {
		ca := testkit.NewTestCA(t, "untrusted CA")
		srv := testkit.NewFakeAPIServerWithCert(testkit.FakeAPIOptions{Token: secretToken}, ca.Issue(t, "127.0.0.1"))
		defer srv.Close()
		other := testkit.NewTestCA(t, "the kubeconfig's CA")
		log, _, err := traceConnect(t, writeTLSKubeconfig(t, tlsCluster{server: srv.URL, caData: other.PEM()}))
		if err == nil {
			t.Fatal("connected")
		}
		text := log.text()
		if !strings.Contains(text, "error: Class: TLS: the server's certificate is signed by an authority this context does not trust") ||
			!strings.Contains(text, "error: Certificate presented: subject ") || !strings.Contains(text, "issuer CN=untrusted CA") ||
			!strings.Contains(text, "names 127.0.0.1") {
			t.Errorf("the picture:\n%s", text)
		}
	})
	t.Run("unauthorized", func(t *testing.T) {
		srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{Token: "another-token-entirely"})
		defer srv.Close()
		path := writeFile(t, "kubeconfig", strings.Replace(kubeconfigFor("x", srv.URL, ""), "users:",
			"    certificate-authority-data: "+base64.StdEncoding.EncodeToString(srv.CAData())+"\nusers:", 1))
		log, _, err := traceConnect(t, path)
		if err == nil {
			t.Fatal("connected")
		}
		text := log.text()
		if !strings.Contains(text, "error: Class: unauthorized") || !strings.Contains(text, "error: HTTP status: 401 Unauthorized") ||
			strings.Contains(text, secretToken) {
			t.Errorf("the picture:\n%s", text)
		}
	})
}

// ErrorPicture names a DNS failure, a timeout and a TLS name mismatch without a network.
func TestErrorPictureClasses(t *testing.T) {
	dns := &url.Error{Op: "Get", URL: "https://user:pw@api.invalid:6443/version?timeout=32s",
		Err: &net.OpError{Op: "dial", Net: "tcp", Err: &net.DNSError{Name: "api.invalid", Err: "no such host", IsNotFound: true}}}
	pic := strings.Join(ErrorPicture(dns), "\n")
	if !strings.Contains(pic, "Class: DNS: the name api.invalid cannot be resolved (no such host)") || strings.Contains(pic, "pw") ||
		strings.Contains(pic, "timeout=32s") {
		t.Errorf("DNS:\n%s", pic)
	}
	refused := &net.OpError{Op: "dial", Net: "tcp", Err: os.NewSyscallError("connect", syscall.ECONNREFUSED)}
	if pic := ErrorPicture(refused); !strings.Contains(pic[0], "TCP: the connection was refused") {
		t.Errorf("refused: %q", pic)
	}
	// Windows' dial fails with Winsock's numbers instead.
	for errno, want := range map[syscall.Errno]string{wsaECONNREFUSED: "TCP: the connection was refused",
		wsaECONNRESET: "TCP: the connection was reset", wsaEHOSTUNREACH: "network: ", wsaENETUNREACH: "network: "} {
		winsock := &net.OpError{Op: "dial", Net: "tcp", Err: os.NewSyscallError("connectex", errno)}
		if pic := ErrorPicture(winsock); !strings.Contains(pic[0], want) {
			t.Errorf("Winsock %d: %q", uint(errno), pic)
		}
	}
	cert := testkit.NewCert(t, testkit.CertSpec{CommonName: "api", DNSNames: []string{"api.example"}})
	parsed, err := x509.ParseCertificate(cert.Cert.Raw)
	if err != nil {
		t.Fatal(err)
	}
	host := x509.HostnameError{Certificate: parsed, Host: "10.0.0.1"}
	pic = strings.Join(ErrorPicture(&url.Error{Op: "Get", URL: "https://10.0.0.1", Err: host}), "\n")
	if !strings.Contains(pic, "Class: TLS: the server's certificate is not issued for 10.0.0.1") || !strings.Contains(pic, "names api.example") {
		t.Errorf("name mismatch:\n%s", pic)
	}
	if pic := ErrorPicture(context.DeadlineExceeded); !strings.Contains(pic[0], "timeout") {
		t.Errorf("timeout: %q", pic)
	}
	plugin := errors.New(`Get "https://x/version": getting credentials: exec: executable /bin/false failed with exit code 1`)
	if pic := ErrorPicture(plugin); !strings.Contains(pic[0], "the credential plugin failed: exec: executable /bin/false failed with exit code 1") {
		t.Errorf("plugin: %q", pic)
	}
}

// A credential plugin's standard error is kept for a failed login only, scrubbed: tokens, URL
// queries, passwords and the kubeconfig's own values are gone, and its standard output (the
// credential) is never seen.
const (
	stderrJWT      = "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJhbGljZSJ9.c2lnbmF0dXJlLXNpZ25hdHVyZQ"
	stderrOpaque   = "ya29Abc123Def456Ghi789Jkl012Mno345Pqr678"
	stderrCode     = "SECRETCODE4711"
	stderrPassword = "hunter2hunter2"
	stdoutToken    = "STDOUT-TOKEN-never-seen-0042"
	envSecret      = "env-secret-value-0815"
)

// stderrPlugin makes a plugin that prints a credential-like text to its standard error, stdout to
// its standard output, and exits with code.
func stderrPlugin(t *testing.T, stderrText string, code int, stdout string) *testkit.Plugin {
	t.Helper()
	p := testkit.NewPlugin(t, "login-plugin")
	p.Output(stdout+"\n", stderrText+"\n", code)
	return p
}

// stderrKubeconfig runs plugin for srv, as command (its path, or its name looked up on PATH), with
// envSecret in its environment.
func stderrKubeconfig(srv *testkit.FakeAPIServer, command string, plugin *testkit.Plugin) string {
	return `apiVersion: v1
kind: Config
current-context: plug
clusters:
- name: fake
  cluster:
    server: ` + srv.URL + `
    certificate-authority-data: ` + base64.StdEncoding.EncodeToString(srv.CAData()) + `
users:
- name: plugin
  user:
    exec:
      apiVersion: client.authentication.k8s.io/v1
      command: ` + testkit.YAMLQuote(command) + `
      interactiveMode: Never
      env:
      - name: LOGIN_SECRET
        value: ` + envSecret + `
` + plugin.EnvYAML("      ") + `contexts:
- name: plug
  context: {cluster: fake, user: plugin}
`
}

func TestPluginStderrOnFailureIsScrubbed(t *testing.T) {
	srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{Token: secretToken})
	defer srv.Close()
	stderr := "error: login failed for alice\n" +
		"token " + stderrJWT + " was rejected\n" +
		"open https://idp.example/authorize?code=" + stderrCode + "&state=x to retry\n" +
		"password=" + stderrPassword + " session " + stderrOpaque + "\n" +
		"the kubeconfig gave " + envSecret
	plugin := stderrPlugin(t, stderr, 1, stdoutToken)
	log, _, err := traceConnect(t, writeFile(t, "kubeconfig", stderrKubeconfig(srv, plugin.Command, plugin)))
	if err == nil {
		t.Fatal("connected with a plugin that failed")
	}
	var pe *PluginError
	if !errors.As(err, &pe) || pe.Stderr == "" {
		t.Fatalf("the error carries no plugin output: %v", err)
	}
	text := log.text()
	for _, want := range []string{
		"login: Login: credential plugin login-plugin (runs without an approval, as kubectl runs it)",
		"error: Class: the credential plugin failed: exec: executable " + plugin.Command + " failed with exit code 1",
		"plugin: The credential plugin's standard error (scrubbed, at most 4 KiB):",
		"plugin:   error: login failed for alice",
		"plugin:   token ••• was rejected",
		"plugin:   open https://idp.example/authorize to retry",
		"plugin:   password=••• session •••",
		"plugin:   the kubeconfig gave •••",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the steps lack %q:\n%s", want, text)
		}
	}
	for _, leak := range []string{stderrJWT, stderrCode, stderrPassword, stderrOpaque, envSecret, stdoutToken, secretToken} {
		if strings.Contains(text, leak) || strings.Contains(err.Error(), leak) {
			t.Errorf("%q reached the log or the error:\n%s\n%v", leak, text, err)
		}
	}
}

// A plugin that works: what it writes to its standard error is not kept, and its token is never
// seen.
func TestPluginStderrOfASuccessfulRunIsNotKept(t *testing.T) {
	srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{Token: secretToken})
	defer srv.Close()
	cred := `{"apiVersion":"client.authentication.k8s.io/v1","kind":"ExecCredential","status":{"token":"` + secretToken + `"}}`
	plugin := stderrPlugin(t, "note: SUCCESS-STDERR-0042 refreshed", 0, cred)
	log, c, err := traceConnect(t, writeFile(t, "kubeconfig", stderrKubeconfig(srv, plugin.Command, plugin)))
	if err != nil {
		t.Fatal(err)
	}
	text := log.text()
	if strings.Contains(text, "SUCCESS-STDERR") || strings.Contains(text, secretToken) {
		t.Errorf("the steps keep the plugin's output:\n%s", text)
	}
	if c.stderr == nil {
		t.Fatal("the plugin's standard error was not captured: client-go's Authenticator changed (pluginstderr.go)")
	}
	if got := c.stderr.since(0); got != "" {
		t.Errorf("the copy of a successful run is still held: %q", got)
	}
	// A heartbeat that works keeps nothing either.
	if err := c.Heartbeat(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// The scrubber's shapes, one by one, and the cut at 4 KiB.
func TestScrubPluginOutput(t *testing.T) {
	for in, want := range map[string]string{
		"Authorization: Bearer abcdef123456":            "Authorization: Bearer •••",
		`{"access_token": "abc123xyz789"}`:              `{"access_token": "•••"}`,
		"client_secret=s3cr3tv4lu3":                     "client_secret=•••",
		"visit https://u:p@login.example/cb?x=1#frag":   "visit https://login.example/cb",
		"-----BEGIN PRIVATE KEY-----\nMIIabc\n-----END": "-----BEGIN … (a PEM block, removed)",
		"exit code 1, try again":                        "exit code 1, try again",
		"known value my-known-secret here":              "known value ••• here",
	} {
		if got := scrubPluginOutput(in, []string{"my-known-secret"}); got != want {
			t.Errorf("scrub(%q) = %q, want %q", in, got, want)
		}
	}
	long := strings.Repeat("plain words ", 1000)
	if got := scrubPluginOutput(long, nil); len(got) > maxPluginStderr+40 || !strings.HasSuffix(got, "(cut at 4 KiB)") {
		t.Errorf("a long text is %d bytes: %q", len(got), got[len(got)-30:])
	}
}
