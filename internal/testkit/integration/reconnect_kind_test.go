//go:build kind

package integration_test

import (
	"context"
	"crypto"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"k8s.io/client-go/tools/clientcmd"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// TestKindReconnectWithARenewedCertificate reproduces the owner's F28 of 2026-10-17 on the kind
// cluster (D-81): a context logs in with a short-lived client certificate in its kubeconfig file
// (client-certificate-data, as a cloud's kubeconfig download holds it), signed by the kind
// cluster's own CA; the certificate expires and the API server rejects it (Login expired); a
// login on the same connection cannot help (nothing re-reads the certificate); the renewed
// certificate is written into the same file, and Log in again, which now connects anew from the
// file, connects with it, the tabs and the view kept. Then Reconnect of a working connection
// swaps it the same way. It reads the CA's key from the control-plane container of our own kind
// cluster, keeps it in memory, and creates nothing in the cluster.
func TestKindReconnectWithARenewedCertificate(t *testing.T) {
	kubeconfig, contextName := kindEnv(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	ca := kindCA(t, contextName)
	server, caData := kindServer(t, kubeconfig, contextName)

	issue := func(valid time.Duration) *testkit.TestCert {
		return testkit.NewCert(t, testkit.CertSpec{CommonName: "cogpit-reconnect-test", Organization: []string{"system:masters"},
			Parent: ca, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(valid),
			ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}})
	}
	file := filepath.Join(t.TempDir(), "renewed.yaml")
	write := func(c *testkit.TestCert) {
		t.Helper()
		doc := fmt.Sprintf(`apiVersion: v1
kind: Config
current-context: reconnect
clusters:
- name: kind
  cluster:
    server: %s
    certificate-authority-data: %s
users:
- name: short-lived
  user:
    client-certificate-data: %s
    client-key-data: %s
contexts:
- name: reconnect
  context: {cluster: kind, user: short-lived}
`, server, base64.StdEncoding.EncodeToString(caData), base64.StdEncoding.EncodeToString(c.PEM()),
			base64.StdEncoding.EncodeToString(c.KeyPEM(t)))
		if err := os.WriteFile(file, []byte(doc), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// Long enough to connect and list the Pods view on a loaded runner, which takes a second or two.
	const shortLife = 8 * time.Second
	write(issue(shortLife))

	s := engine.New(engine.Options{
		Connector:        &engine.ClusterConnector{KubeconfigPaths: []string{file}},
		SnapshotInterval: 20 * time.Millisecond,
		ConnWatch:        engine.ConnWatchOptions{Heartbeat: time.Second, ProbeTimeout: 3 * time.Second, Confirm: 500 * time.Millisecond, LoginWait: 5 * time.Second},
	})
	defer s.Close()
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	if err := s.Connect(ctx, "reconnect"); err != nil {
		t.Fatal(err)
	}
	if c := s.UIState().Connection; c.State != engine.Connected || c.CredentialPlugin {
		t.Fatalf("connected: %+v", c)
	}
	s.OpenTab("pods", true)
	if !testkit.WaitFor(ctx, func() bool { tb := s.Table("pods"); return tb != nil && tb.Synced && len(tb.Rows) > 0 }) {
		t.Fatal("the Pods view did not list")
	}
	tabs := s.UIState().Tabs

	// The certificate expires: the API server rejects it.
	if !testkit.WaitFor(ctx, func() bool { return s.UIState().Connection.State == engine.LoginExpired }) {
		t.Fatalf("still %s after the certificate's end", s.UIState().Connection.State)
	}
	t.Logf("login expired: %s", s.UIState().Connection.Reason)
	// The in-place login of D-58 cannot help: the connection keeps the certificate it read.
	if err := s.LoginContext(""); err != nil {
		t.Fatal(err)
	}
	if !testkit.WaitFor(ctx, func() bool {
		list, _ := s.ConnectAttempts("reconnect", 1) // the in-place login's own attempt, not the expiry's
		return len(list) == 1 && list[0].Kind == engine.AttemptLogin && !list[0].Running && list[0].Outcome == engine.OutcomeFailed &&
			len(list[0].Lines) > 0 && strings.HasPrefix(list[0].Lines[0].Text, "Logging in again (Log in again)") &&
			!s.UIState().Connection.LoggingIn
	}) {
		t.Fatal("the in-place login did not end")
	}
	if st := s.UIState().Connection.State; st != engine.LoginExpired {
		t.Fatalf("the in-place login made it %s", st)
	}

	// The renewed certificate in the same file; Log in again connects anew from it.
	old := s.Backend()
	write(issue(time.Hour))
	var res commands.ConnectionResult
	raw, err := s.Dispatch(ctx, engine.Invocation{ID: "cluster.login", Origin: engine.OriginGUI})
	if err != nil {
		t.Fatalf("Log in again: %v", err)
	}
	res, _ = raw.(commands.ConnectionResult)
	if res.Connection.State != engine.Connected || s.Backend() == old {
		t.Fatalf("after Log in again: %+v, same backend %v", res.Connection, s.Backend() == old)
	}
	u := s.UIState()
	if u.View != "pods" || !slices.EqualFunc(u.Tabs, tabs, func(a, b engine.ViewTab) bool { return a.ID == b.ID }) {
		t.Errorf("the tabs after Log in again: %d (were %d), view %q", len(u.Tabs), len(tabs), u.View)
	}
	if !testkit.WaitFor(ctx, func() bool { tb := s.Table("pods"); return tb != nil && tb.Synced && len(tb.Rows) > 0 }) {
		t.Fatal("the Pods view did not list from the new connection")
	}

	// Reconnect of a working connection: a new backend, the tabs kept.
	second := s.Backend()
	if _, err := s.Dispatch(ctx, engine.Invocation{ID: "cluster.reconnect", Origin: engine.OriginGUI}); err != nil {
		t.Fatalf("Reconnect: %v", err)
	}
	if s.Backend() == second || s.UIState().Connection.State != engine.Connected {
		t.Errorf("after Reconnect: same backend %v, %s", s.Backend() == second, s.UIState().Connection.State)
	}
	list, _ := s.ConnectAttempts("reconnect", 10)
	var kinds []string
	for _, a := range list {
		kinds = append(kinds, a.Kind+":"+a.Outcome)
	}
	t.Logf("the connection log, newest first: %s", strings.Join(kinds, ", "))
}

// kindCA reads the kind cluster's CA certificate and key from its control-plane container (our own
// kind cluster only: kind-<name> runs in <name>-control-plane), or skips the test.
func kindCA(t *testing.T, contextName string) *testkit.TestCert {
	t.Helper()
	node := strings.TrimPrefix(contextName, "kind-") + "-control-plane"
	read := func(path string) []byte {
		out, err := exec.Command("docker", "exec", node, "cat", path).Output() // #nosec G204 -- our kind node's CA, a test's own cluster
		if err != nil {
			t.Skipf("cannot read %s from the kind node %s: %v", path, node, err)
		}
		return out
	}
	certBlock, _ := pem.Decode(read("/etc/kubernetes/pki/ca.crt"))
	keyBlock, _ := pem.Decode(read("/etc/kubernetes/pki/ca.key"))
	if certBlock == nil || keyBlock == nil {
		t.Fatal("the kind CA's files are not PEM")
	}
	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	var key crypto.Signer
	if k, err := x509.ParsePKCS1PrivateKey(keyBlock.Bytes); err == nil {
		key = k
	} else if k, err := x509.ParsePKCS8PrivateKey(keyBlock.Bytes); err == nil {
		key, _ = k.(crypto.Signer)
	} else if k, err := x509.ParseECPrivateKey(keyBlock.Bytes); err == nil {
		key = k
	}
	if key == nil {
		t.Fatal("the kind CA's key cannot be read")
	}
	return &testkit.TestCert{Cert: cert, Key: key}
}

// kindServer is the kind context's API server and the CA its kubeconfig trusts.
func kindServer(t *testing.T, kubeconfig, contextName string) (string, []byte) {
	t.Helper()
	cfg, err := clientcmd.LoadFromFile(kubeconfig)
	if err != nil {
		t.Fatal(err)
	}
	kctx := cfg.Contexts[contextName]
	if kctx == nil || cfg.Clusters[kctx.Cluster] == nil {
		t.Fatalf("no context %s in %s", contextName, kubeconfig)
	}
	c := cfg.Clusters[kctx.Cluster]
	return c.Server, c.CertificateAuthorityData
}
