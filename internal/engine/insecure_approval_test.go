package engine

import (
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/guided-traffic/cogpit/internal/kubeconfig"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// A context whose cluster skips the certificate check asks once before its first connect, the
// answer kept like a credential plugin's approval (A28, owner; H-37).

// approveInsecure allows the unchecked connection of a source context as the window's dialog does:
// the context's pending group, which holds it alone, with the fingerprint the listing showed.
func approveInsecure(t *testing.T, s *Session, id string) {
	t.Helper()
	groups, err := s.PendingExecApprovals(id)
	if err != nil || len(groups) != 1 || len(groups[0].Contexts) != 1 || groups[0].Kind != kubeconfig.LoginInsecure {
		t.Fatalf("pending approvals of %s: %+v, %v", id, groups, err)
	}
	if _, err := s.ApproveExecAll(itemsOf(groups[0])); err != nil {
		t.Fatal(err)
	}
}

// labKubeconfig has three contexts on srv: "lab" and "lab2" skip the certificate check (two
// cluster entries), "checked" trusts the server's CA. labServer replaces lab's server URL.
func labKubeconfig(srv *testkit.FakeAPIServer, labServer string) string {
	return `apiVersion: v1
kind: Config
clusters:
- name: lab
  cluster:
    server: ` + labServer + `
    insecure-skip-tls-verify: true
- name: lab2
  cluster:
    server: ` + srv.URL + `
    insecure-skip-tls-verify: true
- name: checked
  cluster:
    server: ` + srv.URL + `
    certificate-authority-data: ` + base64.StdEncoding.EncodeToString(srv.CAData()) + `
users:
- name: u
  user: {token: ` + sourceSecret + `}
contexts:
- name: lab
  context: {cluster: lab, user: u}
- name: lab2
  context: {cluster: lab2, user: u}
- name: checked
  context: {cluster: checked, user: u}
`
}

// requestsSeen sums the fake server's requests.
func requestsSeen(srv *testkit.FakeAPIServer) int64 {
	var n int64
	for _, c := range srv.Requests() {
		n += c
	}
	return n
}

// TestInsecureContextAsksOnceBeforeItsFirstConnect: the context lists say the unchecked context
// waits (exec required, kind insecure) and the checked one does not; its connect is refused before
// any request reaches the address, naming the flag and the window; the pending listing asks about
// it alone, not with the file's other unchecked context; allowed, it connects, and a restart keeps
// the answer. A changed server, and a proxy Cogpit sets for it, ask again. The approvals file holds
// no credential.
func TestInsecureContextAsksOnceBeforeItsFirstConnect(t *testing.T) {
	srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{})
	defer srv.Close()
	file := filepath.Join(t.TempDir(), "lab.yaml")
	writeUserFile(t, file, labKubeconfig(srv, srv.URL))
	cfg := filepath.Join(t.TempDir(), "Cogpit")
	if _, _, err := NewKubeconfigSources(cfg).AddFile(file); err != nil {
		t.Fatal(err)
	}
	open := func() *Session {
		store := NewClusterSettingsStore(cfg)
		s := New(Options{Connector: &ClusterConnector{Sources: NewKubeconfigSources(cfg), ClusterSettings: store}, ClusterSettings: store})
		t.Cleanup(s.Close)
		return s
	}
	s := open()
	const lab, lab2, checked = "lab [lab.yaml]", "lab2 [lab.yaml]", "checked [lab.yaml]"
	infos := map[string]ContextInfo{}
	list, err := s.Contexts()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range list {
		infos[c.Name] = c
	}
	if c := infos[lab]; c.Exec != ExecRequired || c.ExecKind != kubeconfig.LoginInsecure || !c.Insecure {
		t.Errorf("the unchecked context: %+v", c)
	}
	if c := infos[checked]; c.Exec != "" || c.Insecure {
		t.Errorf("the checked context: %+v", c)
	}

	err = s.Connect(t.Context(), lab)
	if !errors.Is(err, ErrExecNotApproved) || !strings.Contains(err.Error(), "insecure-skip-tls-verify") ||
		!strings.Contains(err.Error(), "navigator's clusters in Cogpit's window (⌘P)") {
		t.Fatalf("first connect: %v", err)
	}
	if n := requestsSeen(srv); n != 0 {
		t.Fatalf("refused, yet %d requests reached the address", n)
	}
	if u := s.UIState(); u.Connection.State != ConnectError {
		t.Errorf("connection %+v", u.Connection)
	}
	if err := s.Connect(t.Context(), checked); err != nil {
		t.Fatalf("the checked context asks nothing: %v", err)
	}

	all, err := s.PendingExecApprovals("")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("want two groups of one context each (lab, lab2), got %+v", all)
	}
	for _, g := range all {
		if g.Kind != kubeconfig.LoginInsecure || len(g.Contexts) != 1 || g.Plugin.Command != "" || !g.Contexts[0].TLS.Insecure {
			t.Errorf("group %+v", g)
		}
	}
	approveInsecure(t, s, lab)
	if err := s.Connect(t.Context(), lab); err != nil {
		t.Fatalf("allowed: %v", err)
	}
	if info, err := s.ExecPlugin(lab2); err != nil || info.State != ExecRequired {
		t.Errorf("allowing lab allowed lab2 too: %+v, %v", info, err)
	}

	s2 := open() // a restart: asked once, remembered
	if info, err := s2.ExecPlugin(lab); err != nil || info.State != ExecApproved || info.Kind != kubeconfig.LoginInsecure {
		t.Fatalf("after a restart: %+v, %v", info, err)
	}
	if err := s2.Connect(t.Context(), lab); err != nil {
		t.Fatalf("after a restart: %v", err)
	}

	// Cogpit's own proxy for the context is bound too (A23).
	if _, err := s2.ChangeClusterSettings(lab, func(cs *ClusterSettings) error { cs.Proxy = "http://proxy.example:3128"; return nil }); err != nil {
		t.Fatal(err)
	}
	if info, _ := s2.ExecPlugin(lab); info == nil || info.State != ExecChanged {
		t.Errorf("after a proxy was set: %+v", info)
	}
	if _, err := s2.ChangeClusterSettings(lab, func(cs *ClusterSettings) error { cs.Proxy = ""; return nil }); err != nil {
		t.Fatal(err)
	}

	// Another server at the context's name asks again.
	writeUserFile(t, file, labKubeconfig(srv, "https://impostor.example:6443"))
	if info, _ := s2.ExecPlugin(lab); info == nil || info.State != ExecChanged {
		t.Fatalf("after the server changed: %+v", info)
	}
	if err := s2.Disconnect(); err != nil { // a connect again would only show the open connection (D-69)
		t.Fatal(err)
	}
	if err := s2.Connect(t.Context(), lab); !errors.Is(err, ErrExecNotApproved) || !strings.Contains(err.Error(), "changed") {
		t.Errorf("connect after the server changed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(cfg, ExecApprovalsFile))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), sourceSecret) || !strings.Contains(string(data), `"kind": "insecure"`) {
		t.Errorf("approvals file:\n%s", data)
	}
}

// TestInsecureContextWithAPluginIsAskedInThePluginsDialog: a context that runs a credential plugin
// and skips the check has one approval, the plugin's, whose listing carries the flag (the dialog
// warns about it); its kind stays the plugin's.
func TestInsecureContextWithAPluginIsAskedInThePluginsDialog(t *testing.T) {
	doc := strings.Replace(execSourceKubeconfig("eks", "aws", "eks", "get-token"),
		`cluster: {server: "https://exec.example:6443"}`, "cluster: {server: \"https://exec.example:6443\", insecure-skip-tls-verify: true}", 1)
	file := filepath.Join(t.TempDir(), "eks.yaml")
	writeUserFile(t, file, doc)
	k := NewKubeconfigSources(filepath.Join(t.TempDir(), "Cogpit"))
	if _, _, err := k.AddFile(file); err != nil {
		t.Fatal(err)
	}
	s := New(Options{Connector: &ClusterConnector{Sources: k}})
	t.Cleanup(s.Close)
	groups, err := s.PendingExecApprovals("eks [eks.yaml]")
	if err != nil || len(groups) != 1 {
		t.Fatalf("groups %+v, %v", groups, err)
	}
	g := groups[0]
	if g.Kind != kubeconfig.LoginProgram || len(g.Contexts) != 1 || !g.Contexts[0].TLS.Insecure {
		t.Errorf("group %+v", g)
	}
}

// TestInsecureContextWithoutAUserEntry: a context that names no existing user logs in with
// nothing, and still asks once when its cluster skips the check, at listing and connect alike.
func TestInsecureContextWithoutAUserEntry(t *testing.T) {
	srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{})
	defer srv.Close()
	file := filepath.Join(t.TempDir(), "anon.yaml")
	writeUserFile(t, file, `apiVersion: v1
kind: Config
clusters:
- name: lab
  cluster: {server: "`+srv.URL+`", insecure-skip-tls-verify: true}
contexts:
- name: anon
  context: {cluster: lab, user: nobody}
`)
	k := NewKubeconfigSources(filepath.Join(t.TempDir(), "Cogpit"))
	if _, _, err := k.AddFile(file); err != nil {
		t.Fatal(err)
	}
	s := New(Options{Connector: &ClusterConnector{Sources: k}})
	t.Cleanup(s.Close)
	const id = "anon [anon.yaml]"
	if err := s.Connect(t.Context(), id); !errors.Is(err, ErrExecNotApproved) {
		t.Fatalf("first connect: %v", err)
	}
	approveInsecure(t, s, id)
	if err := s.Connect(t.Context(), id); err != nil {
		t.Fatalf("allowed: %v", err)
	}
}
