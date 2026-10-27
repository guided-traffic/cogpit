package testkit

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

// LoginPlugin is a credential plugin named login-plugin for tests of a login that waits (D-73),
// and a kubeconfig (Kubeconfig) whose one context logs in with it against a fake API server.
type LoginPlugin struct {
	*Plugin
	Kubeconfig string
}

// NewLoginPlugin makes the plugin and a kubeconfig of context contextName that runs it against
// srv. The plugin hands out nothing until Give.
func NewLoginPlugin(t testing.TB, srv *FakeAPIServer, contextName string) *LoginPlugin {
	t.Helper()
	p := &LoginPlugin{Plugin: NewPlugin(t, "login-plugin"), Kubeconfig: filepath.Join(t.TempDir(), contextName+".yaml")}
	doc := `apiVersion: v1
kind: Config
clusters:
- name: ` + contextName + `-cluster
  cluster:
    server: ` + srv.URL + `
    certificate-authority-data: ` + base64.StdEncoding.EncodeToString(srv.CAData()) + `
users:
- name: ` + contextName + `-plugin
  user:
    exec:
      apiVersion: client.authentication.k8s.io/v1
      command: ` + YAMLQuote(p.Command) + `
      interactiveMode: IfAvailable
      env:
` + p.EnvYAML("      ") + `contexts:
- name: ` + contextName + `
  context: {cluster: ` + contextName + `-cluster, user: ` + contextName + `-plugin}
`
	if err := os.WriteFile(p.Kubeconfig, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}
