package commands_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// A28 (owner; H-37) per origin: an agent's or the CLI's cluster_connect of a context that skips the
// certificate check is refused until the user allowed it in the window, as for an unapproved
// credential plugin; only the window allows it (kubeconfig_approveExec is GUI only); once allowed,
// every origin connects.
func TestInsecureContextConnectsPerOrigin(t *testing.T) {
	srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{})
	defer srv.Close()
	file := filepath.Join(t.TempDir(), "lab.yaml")
	doc := `apiVersion: v1
kind: Config
clusters:
- name: lab
  cluster: {server: "` + srv.URL + `", insecure-skip-tls-verify: true}
users:
- name: u
  user: {token: tok-SECRET-lab}
contexts:
- name: lab
  context: {cluster: lab, user: u}
`
	if err := os.WriteFile(file, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	k := engine.NewKubeconfigSources(filepath.Join(t.TempDir(), "Cogpit"))
	if _, _, err := k.AddFile(file); err != nil {
		t.Fatal(err)
	}
	s := engine.New(engine.Options{Connector: &engine.ClusterConnector{Sources: k}})
	t.Cleanup(s.Close)
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	h := &harness{s: s}
	const id = "lab [lab.yaml]"

	for _, origin := range []engine.Origin{engine.OriginExternalAgent, engine.OriginBuiltInAgent, engine.OriginCLI} {
		var res commands.ConnectResult
		h.must(t, origin, "cluster.connect", map[string]string{"context": id}, &res)
		if res.State != engine.ConnectError || !strings.Contains(res.Error, "insecure-skip-tls-verify") ||
			!strings.Contains(res.Error, "navigator's clusters in Cogpit's window (⌘P)") || strings.Contains(res.Error, "tok-SECRET") {
			t.Errorf("%s: %+v", origin, res)
		}
	}

	var pending commands.PendingResult
	h.must(t, engine.OriginExternalAgent, "kubeconfig.pendingApprovals", map[string]string{"context": id}, &pending)
	if len(pending.Groups) != 1 || pending.Groups[0].Kind != "insecure" || len(pending.Groups[0].Contexts) != 1 {
		t.Fatalf("pending %+v", pending)
	}
	approve := map[string]any{"approvals": []map[string]string{{"context": id, "fingerprint": pending.Groups[0].Contexts[0].Fingerprint}}}
	for _, origin := range []engine.Origin{engine.OriginExternalAgent, engine.OriginBuiltInAgent, engine.OriginCLI, engine.OriginDeepLink} {
		if err := h.run(t, origin, "kubeconfig.approveExec", approve, nil); err == nil {
			t.Errorf("%s allowed the unchecked connection", origin)
		} else if !errors.Is(err, engine.ErrGUIOnly) && !errors.Is(err, engine.ErrDeepLinkRefused) {
			t.Logf("%s refused with %v", origin, err)
		}
	}
	h.must(t, engine.OriginGUI, "kubeconfig.approveExec", approve, nil)

	var res commands.ConnectResult
	h.must(t, engine.OriginExternalAgent, "cluster.connect", map[string]string{"context": id}, &res)
	if res.State != engine.Connected || !res.TLSUnchecked {
		t.Errorf("allowed, an agent's connect: %+v", res)
	}
	// A29: agents read what the status bar shows, from the engine's state.
	var u struct {
		Connection struct {
			State        string `json:"state"`
			TLSUnchecked bool   `json:"tlsUnchecked"`
		} `json:"connection"`
	}
	h.must(t, engine.OriginExternalAgent, "ui.state", map[string]any{}, &u)
	if u.Connection.State != string(engine.Connected) || !u.Connection.TLSUnchecked {
		t.Errorf("ui_state's connection: %+v", u.Connection)
	}
	h.must(t, engine.OriginGUI, "cluster.disconnect", map[string]any{}, nil)
	u.Connection.TLSUnchecked = false // left out of the JSON when false: decode into a clean field
	h.must(t, engine.OriginExternalAgent, "ui.state", map[string]any{}, &u)
	if u.Connection.TLSUnchecked {
		t.Errorf("disconnected, ui_state still says TLS is not checked: %+v", u.Connection)
	}
}
