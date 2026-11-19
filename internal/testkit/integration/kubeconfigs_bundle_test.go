package integration_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
)

// TestPendingApprovalsAreBundledAndApprovedByTheGUIOnly (D-28): two contexts of one file that run
// the same plugin are one group in kubeconfig.pendingApprovals; an agent may read it (the arguments
// withheld, no environment value) but not approve; the GUI's approval is refused as a whole, naming
// the context, when one fingerprint is stale, and allows both otherwise.
func TestPendingApprovalsAreBundledAndApprovedByTheGUIOnly(t *testing.T) {
	const secret = "env-value-must-not-leak"
	file := filepath.Join(t.TempDir(), "eks.yaml")
	yaml := `apiVersion: v1
kind: Config
clusters:
- {name: one, cluster: {server: "https://one.example:443"}}
- {name: two, cluster: {server: "https://two.example:443"}}
users:
- name: u
  user:
    exec:
      apiVersion: client.authentication.k8s.io/v1
      command: aws
      args: [eks, get-token, --cluster-name, prod]
      interactiveMode: Never
      env:
      - {name: AWS_PROFILE, value: ` + secret + `}
contexts:
- {name: one, context: {cluster: one, user: u}}
- {name: two, context: {cluster: two, user: u}}
`
	if err := os.WriteFile(file, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	s, _, _ := sourcesSession(t)
	if err := sourcesDispatch(t, s, engine.OriginGUI, "kubeconfig.addFile", map[string]string{"path": file}, nil); err != nil {
		t.Fatal(err)
	}

	var forAgent, forGUI commands.PendingResult
	if err := sourcesDispatch(t, s, engine.OriginExternalAgent, "kubeconfig.pendingApprovals", struct{}{}, &forAgent); err != nil {
		t.Fatal(err)
	}
	if len(forAgent.Groups) != 1 || len(forAgent.Groups[0].Contexts) != 2 {
		t.Fatalf("groups for an agent: %+v", forAgent)
	}
	g := forAgent.Groups[0]
	if g.ArgsWithheld != 4 || g.Plugin.Args != nil || !slices.Equal(g.Plugin.EnvNames, []string{"AWS_PROFILE"}) {
		t.Errorf("the agent's group: %+v", g)
	}
	if out, _ := json.Marshal(forAgent); strings.Contains(string(out), secret) {
		t.Errorf("the plugin's environment value reached the agent: %s", out)
	}
	if err := sourcesDispatch(t, s, engine.OriginGUI, "kubeconfig.pendingApprovals", map[string]string{"context": "two [eks.yaml]"}, &forGUI); err != nil {
		t.Fatal(err)
	}
	if len(forGUI.Groups) != 1 || forGUI.Groups[0].ArgsWithheld != 0 || len(forGUI.Groups[0].Plugin.Args) != 4 {
		t.Fatalf("groups for the GUI: %+v", forGUI)
	}
	var items []map[string]string
	for _, c := range forGUI.Groups[0].Contexts {
		items = append(items, map[string]string{"context": c.Context, "fingerprint": c.Fingerprint})
	}
	good := map[string]any{"approvals": items}

	for _, origin := range []engine.Origin{engine.OriginExternalAgent, engine.OriginBuiltInAgent, engine.OriginCLI} {
		if err := sourcesDispatch(t, s, origin, "kubeconfig.approveExec", good, nil); !errors.Is(err, engine.ErrGUIOnly) {
			t.Errorf("approveExec from %s: %v, want ErrGUIOnly", origin, err)
		}
	}
	stale := map[string]any{"approvals": []map[string]string{items[0], {"context": items[1]["context"], "fingerprint": "sha256:stale"}}}
	err := sourcesDispatch(t, s, engine.OriginGUI, "kubeconfig.approveExec", stale, nil)
	if !errors.Is(err, engine.ErrExecChangedSinceShown) || !strings.Contains(err.Error(), "two [eks.yaml]") || strings.Contains(err.Error(), "one [eks.yaml]") {
		t.Fatalf("a stale fingerprint: %v", err)
	}
	for id, c := range contextIDs(t, s) {
		if c.Exec == engine.ExecApproved {
			t.Errorf("%s was approved by a refused call", id)
		}
	}

	var res commands.ApprovedResult
	if err := sourcesDispatch(t, s, engine.OriginGUI, "kubeconfig.approveExec", good, &res); err != nil || len(res.Approved) != 2 {
		t.Fatalf("approveExec: %+v, %v", res, err)
	}
	for id, c := range contextIDs(t, s) {
		if c.Exec != engine.ExecApproved {
			t.Errorf("%s is %q after the approval", id, c.Exec)
		}
	}
	var after commands.PendingResult
	if err := sourcesDispatch(t, s, engine.OriginExternalAgent, "kubeconfig.pendingApprovals", struct{}{}, &after); err != nil || len(after.Groups) != 0 {
		t.Errorf("still pending: %+v, %v", after, err)
	}
}
