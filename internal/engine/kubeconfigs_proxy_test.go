package engine

import (
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/testkit"
)

// A credential plugin's approval binds the proxy Cogpit itself sets for the cluster (A23, owner
// 2026-10-14 evening; D-26, H-34): the dialog's listing names it, its password as •••, and a
// changed proxy asks again, at listing and at connect time alike.

// proxyPluginKubeconfig is a kubeconfig for srv whose user runs plugin.
func proxyPluginKubeconfig(srv *testkit.FakeAPIServer, plugin *testkit.Plugin) string {
	return `apiVersion: v1
kind: Config
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
      command: ` + testkit.YAMLQuote(plugin.Command) + `
      interactiveMode: Never
      env:
` + plugin.EnvYAML("      ") + `contexts:
- name: eks
  context: {cluster: fake, user: plugin}
`
}

// TestPluginApprovalBindsTheClusterProxy: approved without a proxy, the plugin runs and the
// connection goes straight to the server. Once the user sets a proxy in Cluster Settings the
// approval no longer matches: the contexts list it as changed (and say so with EventContexts), the
// pending listing names the proxy with its password as •••, and a connect is refused before the
// plugin runs. Allowed again for that proxy, the connect runs the plugin through the proxy. A new
// password alone keeps the approval; a new host asks again. No password reaches the listing or
// the approvals file.
func TestPluginApprovalBindsTheClusterProxy(t *testing.T) {
	srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{})
	defer srv.Close()
	plugin := testkit.NewPlugin(t, "login-plugin")
	plugin.Give("tok", time.Time{})
	file := filepath.Join(t.TempDir(), "eks.yaml")
	writeUserFile(t, file, proxyPluginKubeconfig(srv, plugin))
	cfg := filepath.Join(t.TempDir(), "Cogpit")
	k := NewKubeconfigSources(cfg)
	if _, _, err := k.AddFile(file); err != nil {
		t.Fatal(err)
	}
	store := NewClusterSettingsStore(cfg)
	s := New(Options{Connector: &ClusterConnector{Sources: k, ClusterSettings: store}, ClusterSettings: store})
	t.Cleanup(s.Close)
	const id = "eks [eks.yaml]"
	var contextsEvents atomic.Int32
	defer s.Subscribe(func(e Event) {
		if e.Type == EventContexts {
			contextsEvents.Add(1)
		}
	})()

	state := func() string {
		t.Helper()
		info, err := s.ExecPlugin(id)
		if err != nil {
			t.Fatal(err)
		}
		return info.State
	}
	approve := func() ExecApprovalContext {
		t.Helper()
		groups, err := s.PendingExecApprovals(id)
		if err != nil || len(groups) != 1 || len(groups[0].Contexts) != 1 {
			t.Fatalf("pending %+v, %v", groups, err)
		}
		c := groups[0].Contexts[0]
		if _, err := s.ApproveExecAll([]ExecApprovalItem{{Context: id, Fingerprint: c.Fingerprint}}); err != nil {
			t.Fatal(err)
		}
		return c
	}
	setProxy := func(raw string) {
		t.Helper()
		if _, err := s.ChangeClusterSettings(id, func(cs *ClusterSettings) error { cs.Proxy = raw; return nil }); err != nil {
			t.Fatal(err)
		}
	}

	if c := approve(); c.TLS.ClusterProxy != "" {
		t.Errorf("no proxy set, the listing names %q", c.TLS.ClusterProxy)
	}
	if err := s.Connect(t.Context(), id); err != nil {
		t.Fatalf("connect without a proxy: %v", err)
	}
	if plugin.Runs() != 1 {
		t.Fatalf("the plugin ran %d times", plugin.Runs())
	}

	proxy := testkit.NewForwardProxy(t, "alice", proxySecret)
	before := contextsEvents.Load()
	setProxy(proxy.URLWithUser("alice", proxySecret).String())
	if contextsEvents.Load() == before {
		t.Error("changing the proxy did not tell the context lists")
	}
	if got := state(); got != ExecChanged {
		t.Fatalf("after the proxy was set: state %s, want changed", got)
	}
	if err := s.Disconnect(); err != nil { // a connect again would only show the open connection (D-69)
		t.Fatal(err)
	}
	err := s.Connect(t.Context(), id)
	if !errors.Is(err, ErrExecNotApproved) {
		t.Fatalf("connect through a proxy the approval did not name: %v", err)
	}
	if plugin.Runs() != 1 || proxy.Total() != 0 {
		t.Fatalf("refused, yet the plugin ran (%d) or the proxy was used (%d)", plugin.Runs(), proxy.Total())
	}

	c := approve()
	if want := "http://alice:•••@" + proxy.URL.Host; c.TLS.ClusterProxy != want {
		t.Errorf("the listing names the proxy %q, want %q", c.TLS.ClusterProxy, want)
	}
	if err := s.Connect(t.Context(), id); err != nil {
		t.Fatalf("connect through the allowed proxy: %v", err)
	}
	// client-go keeps the plugin's unexpired credential in the process, so it need not run again;
	// what matters is that the policy let the connect through, and that it went through the proxy.
	if proxy.Total() == 0 {
		t.Errorf("allowed: the proxy saw no request")
	}

	// A new password alone: the proxy is the same, the approval stands (the lead's choice, A23).
	setProxy(proxy.URLWithUser("alice", "rotated-"+proxySecret).String())
	if got := state(); got != ExecApproved {
		t.Errorf("after a new password: state %s, want approved", got)
	}
	// Another host or user asks again.
	for _, other := range []string{"http://alice:x@proxy.example:3128", proxy.URLWithUser("bob", proxySecret).String()} {
		setProxy(other)
		if got := state(); got != ExecChanged {
			t.Errorf("proxy %s: state %s, want changed", RedactProxyURL(other), got)
		}
	}
	// Cleared: the approval given without a proxy, long replaced, does not come back.
	setProxy("")
	if got := state(); got != ExecChanged {
		t.Errorf("proxy cleared: state %s, want changed (the last approval named a proxy)", got)
	}

	groups, err := s.PendingExecApprovals("")
	if err != nil {
		t.Fatal(err)
	}
	approvals, err := os.ReadFile(filepath.Join(cfg, ExecApprovalsFile))
	if err != nil {
		t.Fatal(err)
	}
	for what, text := range map[string]string{"listing": mustMarshal(t, groups), "approvals file": string(approvals)} {
		if strings.Contains(text, proxySecret) {
			t.Errorf("the %s carries the proxy's password", what)
		}
	}
}

// TestApprovalWithoutAClusterProxyStillMatches: an approval given before Cogpit's proxy was bound
// (its fingerprint has no proxy of Cogpit's) still matches for a context that has none, so the
// upgrade asks again only where a proxy is set.
func TestApprovalWithoutAClusterProxyStillMatches(t *testing.T) {
	b := newBundleSession(t, eks("plain", getToken...))
	const id = "plain [config]"
	groups := b.pending(t, id)
	if len(groups) != 1 {
		t.Fatalf("groups %+v", groups)
	}
	// The connector of newBundleSession has no cluster settings: the fingerprint is the one an
	// earlier build computed. Approved there, a connector with settings but no proxy agrees.
	if _, err := b.s.ApproveExecAll(itemsOf(groups[0])); err != nil {
		t.Fatal(err)
	}
	store := NewClusterSettingsStore(filepath.Join(t.TempDir(), "Cogpit"))
	c := &ClusterConnector{Sources: b.k, ClusterSettings: store}
	s := New(Options{Connector: c, ClusterSettings: store})
	t.Cleanup(s.Close)
	if info, err := s.ExecPlugin(id); err != nil || info.State != ExecApproved {
		t.Errorf("with settings but no proxy: %+v, %v", info, err)
	}
}
