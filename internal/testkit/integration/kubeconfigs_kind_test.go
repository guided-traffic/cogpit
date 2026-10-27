//go:build kind

package integration_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/kubeconfig"
)

// TestKindAddedKubeconfig adds the kind cluster's kubeconfig as a file source, connects through it
// and removes the source again. The kind kubeconfig is only read: it stays byte-identical, with
// its time and mode. Nothing is written to the cluster.
func TestKindAddedKubeconfig(t *testing.T) {
	kubeconfig, contextName := os.Getenv("COGPIT_KIND_KUBECONFIG"), os.Getenv("COGPIT_KIND_CONTEXT")
	if kubeconfig == "" || contextName == "" {
		t.Skip("COGPIT_KIND_KUBECONFIG and COGPIT_KIND_CONTEXT are not set; run make test-kind")
	}
	before, err := os.ReadFile(kubeconfig)
	if err != nil {
		t.Fatal(err)
	}
	statBefore, err := os.Stat(kubeconfig)
	if err != nil {
		t.Fatal(err)
	}

	s, _, audit := sourcesSession(t)
	var added commands.SourceAdded
	if err := sourcesDispatch(t, s, engine.OriginGUI, "kubeconfig.addFile", map[string]string{"path": kubeconfig}, &added); err != nil {
		t.Fatal(err)
	}
	id := contextName + " [" + added.Source.ID + "]"
	c, ok := contextIDs(t, s)[id]
	if !ok || c.KubeContext != contextName {
		t.Fatalf("no context %q: %+v", id, contextIDs(t, s))
	}
	if c.File != kubeconfig {
		t.Fatalf("the context reads %s, not the added file %s", c.File, kubeconfig)
	}

	var res commands.ConnectResult
	if err := sourcesDispatch(t, s, engine.OriginExternalAgent, "cluster.connect", map[string]string{"context": id}, &res); err != nil {
		t.Fatal(err)
	}
	if res.State != engine.Connected {
		t.Fatalf("connect through the added file: %+v", res)
	}
	b := s.Backend()
	if b == nil || b.Context != id || b.Conn.Context() != contextName || b.Conn.ServerVersion() == "" {
		t.Fatalf("backend %+v", b)
	}
	t.Logf("connected to %s through %s: Kubernetes %s at %s", contextName, c.File, b.Conn.ServerVersion(), b.Conn.Server())

	if err := sourcesDispatch(t, s, engine.OriginGUI, "kubeconfig.remove", map[string]string{"source": added.Source.ID}, nil); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(kubeconfig)
	if err != nil {
		t.Fatal(err)
	}
	statAfter, err := os.Stat(kubeconfig)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) || !statAfter.ModTime().Equal(statBefore.ModTime()) || statAfter.Mode() != statBefore.Mode() {
		t.Error("the kind kubeconfig changed")
	}
	if bytes.Contains([]byte(audit.String()), []byte("client-key-data")) {
		t.Error("the audit log carries the kubeconfig")
	}
}

// TestKindExecPluginAfterApproval authenticates to the kind cluster through an exec credential
// plugin: a kubeconfig file added in Cogpit whose user runs a script that hands back the kind
// user's client certificate as an ExecCredential. Connecting is refused until the user allowed the
// plugin in the GUI; then the plugin runs and the API server accepts its certificate.
func TestKindExecPluginAfterApproval(t *testing.T) {
	kubeconfigPath, contextName := os.Getenv("COGPIT_KIND_KUBECONFIG"), os.Getenv("COGPIT_KIND_CONTEXT")
	if kubeconfigPath == "" || contextName == "" {
		t.Skip("COGPIT_KIND_KUBECONFIG and COGPIT_KIND_CONTEXT are not set; run make test-kind")
	}
	data, err := os.ReadFile(kubeconfigPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := kubeconfig.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	kctx := cfg.Contexts[contextName]
	if kctx == nil || cfg.Clusters[kctx.Cluster] == nil || cfg.AuthInfos[kctx.AuthInfo] == nil {
		t.Fatalf("no context %s in the kind kubeconfig", contextName)
	}
	cl, user := cfg.Clusters[kctx.Cluster], cfg.AuthInfos[kctx.AuthInfo]

	dir := t.TempDir()
	cred, err := json.Marshal(map[string]any{
		"apiVersion": "client.authentication.k8s.io/v1", "kind": "ExecCredential",
		"status": map[string]string{"clientCertificateData": string(user.ClientCertificateData), "clientKeyData": string(user.ClientKeyData)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "credential.json"), cred, 0o600); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(dir, "marker")
	script := "#!/bin/sh\necho ran >> \"$COGPIT_TEST_MARKER\"\ncat \"$COGPIT_TEST_CREDENTIAL\"\n"
	if err := os.WriteFile(filepath.Join(dir, "kind-plugin"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "kind-exec.yaml")
	doc := `apiVersion: v1
kind: Config
clusters:
- name: kind
  cluster:
    server: ` + cl.Server + `
    certificate-authority-data: ` + base64.StdEncoding.EncodeToString(cl.CertificateAuthorityData) + `
users:
- name: plugin
  user:
    exec:
      apiVersion: client.authentication.k8s.io/v1
      command: ./kind-plugin
      interactiveMode: Never
      env:
      - {name: COGPIT_TEST_MARKER, value: "` + marker + `"}
      - {name: COGPIT_TEST_CREDENTIAL, value: "` + filepath.Join(dir, "credential.json") + `"}
contexts:
- name: kind-exec
  context: {cluster: kind, user: plugin}
`
	if err := os.WriteFile(file, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}

	s, _, _ := sourcesSession(t)
	if err := sourcesDispatch(t, s, engine.OriginGUI, "kubeconfig.addFile", map[string]string{"path": file}, nil); err != nil {
		t.Fatal(err)
	}
	id := "kind-exec [kind-exec.yaml]"
	var res commands.ConnectResult
	if err := sourcesDispatch(t, s, engine.OriginGUI, "cluster.connect", map[string]string{"context": id}, &res); err != nil {
		t.Fatal(err)
	}
	if res.State != engine.ConnectError || pluginRuns(t, marker) != 0 {
		t.Fatalf("connect before approval: %+v, plugin runs %d", res, pluginRuns(t, marker))
	}
	var info commands.ExecPluginResult
	if err := sourcesDispatch(t, s, engine.OriginGUI, "kubeconfig.execPlugin", map[string]string{"context": id}, &info); err != nil {
		t.Fatal(err)
	}
	if err := sourcesDispatch(t, s, engine.OriginGUI, "kubeconfig.approveExec",
		approvals(id, info.Fingerprint), nil); err != nil {
		t.Fatal(err)
	}
	if err := sourcesDispatch(t, s, engine.OriginGUI, "cluster.connect", map[string]string{"context": id}, &res); err != nil {
		t.Fatal(err)
	}
	if res.State != engine.Connected || pluginRuns(t, marker) < 1 {
		t.Fatalf("connect after approval: %+v, plugin runs %d", res, pluginRuns(t, marker))
	}
	t.Logf("connected through the exec plugin: Kubernetes %s", s.Backend().Conn.ServerVersion())
}

// pluginRuns is how often the plugin noted a run in marker.
func pluginRuns(t *testing.T, marker string) int {
	t.Helper()
	data, err := os.ReadFile(marker)
	if errors.Is(err, os.ErrNotExist) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Count(string(data), "ran")
}
