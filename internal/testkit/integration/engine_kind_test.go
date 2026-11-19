//go:build kind

package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// TestKindEngineWorkflows walks the MVP's kind workflows through the engine without the window,
// against the seeded kind cluster (make kind-up): every view fills from watches, the agent
// showcase finds and opens the Service with ClusterIP 10.96.100.100 with its YAML, logs stream
// from a pod, the pod shell runs a command, and kubectl in Cogpit's terminal lists the cluster's
// pods. It writes nothing to the cluster.
func TestKindEngineWorkflows(t *testing.T) {
	kubeconfig, contextName := os.Getenv("COGPIT_KIND_KUBECONFIG"), os.Getenv("COGPIT_KIND_CONTEXT")
	if kubeconfig == "" || contextName == "" {
		t.Skip("COGPIT_KIND_KUBECONFIG and COGPIT_KIND_CONTEXT are not set; run make test-kind")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	s := engine.New(engine.Options{Connector: &engine.ClusterConnector{KubeconfigPaths: []string{kubeconfig}},
		Kubectl: engine.NewKubectl(t.TempDir(), engine.KubectlHooks{})}) // an empty directory for binaries: nothing is downloaded
	defer s.Close()
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	if err := s.Connect(ctx, contextName); err != nil {
		t.Fatal(err)
	}
	dispatch := func(id string, params any) any {
		t.Helper()
		raw, err := json.Marshal(params)
		if err != nil {
			t.Fatal(err)
		}
		res, err := s.Dispatch(ctx, engine.Invocation{ID: id, Params: raw, Origin: engine.OriginGUI})
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		return res
	}

	// Daemon sets come with kind (kube-proxy, kindnet); replica sets and the job with the seed.
	for _, view := range []string{"nodes", "pods", "deployments", "daemonsets", "replicasets", "jobs", "services", "namespaces", "events"} {
		dispatch("ui.openView", map[string]any{"view": view})
		if !testkit.WaitFor(ctx, func() bool {
			tbl := s.Table(view)
			return tbl != nil && tbl.Synced && tbl.Total > 0
		}) {
			t.Fatalf("view %s never filled: %+v", view, s.Table(view))
		}
		t.Logf("view %s: %d rows", view, s.Table(view).Total)
	}

	found := dispatch("resources.find", map[string]any{"kind": "services", "field": "clusterIP", "value": "10.96.100.100"}).(commands.FindResult)
	if found.Count != 1 || found.Matches[0].Ref.Name != "web" {
		t.Fatalf("resources.find: %+v", found)
	}
	dispatch("ui.navigate", map[string]any{"ref": found.Matches[0].Ref})
	detail, err := s.DetailWait(ctx, found.Matches[0].Ref)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(detail.YAML, "clusterIP: 10.96.100.100") {
		t.Errorf("drawer YAML lacks the cluster IP:\n%s", detail.YAML)
	}

	// Logs of the crash-looping pod, which prints "starting" on every start.
	pods := dispatch("resources.list", map[string]any{"kind": "pods", "namespaces": []string{"cogpit-demo"}, "filter": "crashloop"}).(commands.ListResult)
	if len(pods.Rows) == 0 {
		t.Fatal("no crashloop pod in cogpit-demo; run make kind-seed")
	}
	logsTab := dispatch("pod.logs", map[string]any{"ref": pods.RefAt(0)}).(commands.DockResult)
	logs, err := s.OpenLogs(ctx, logsTab.Tab.ID)
	if err != nil {
		t.Fatal(err)
	}
	if out := readUntil(t, logs, "starting", 30*time.Second); !strings.Contains(out, "starting") {
		t.Errorf("logs: %q", out)
	}
	_ = logs.Close()

	// A shell in the toolbox pod.
	toolbox := model.ObjectRef{Kind: "pods", Namespace: "cogpit-demo", Name: "toolbox"}
	shellTab := dispatch("pod.shell", map[string]any{"ref": toolbox}).(commands.DockResult)
	shell, err := s.OpenTerminal(ctx, shellTab.Tab.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(shell, "echo cogpit-$((40+2))\n"); err != nil {
		t.Fatal(err)
	}
	if out := readUntil(t, shell, "cogpit-42", 30*time.Second); !strings.Contains(out, "cogpit-42") {
		t.Errorf("pod shell: %q", out)
	}
	_ = shell.Close()

	// kubectl in Cogpit's local terminal talks to the kind cluster through the terminal's own
	// kubeconfig.
	if _, err := exec.LookPath("kubectl"); err != nil {
		t.Log("kubectl not on PATH; terminal check skipped")
		return
	}
	termTab := dispatch("terminal.open", map[string]any{}).(commands.DockResult)
	term, err := s.OpenTerminal(ctx, termTab.Tab.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = term.Close() }()
	// D-3: the terminal's kubectl was checked against kind's version from connect; nothing was
	// downloaded, so nothing goes first, and a skewed kubectl is only offered a release.
	if tab, _ := s.UIState().DockTab(termTab.Tab.ID); tab.Kubectl == nil || tab.Kubectl.Server == "" ||
		!strings.HasPrefix(s.Backend().Conn.ServerVersion(), "v"+tab.Kubectl.Server) || tab.Kubectl.Found == "" ||
		tab.Kubectl.First != "" || tab.Kubectl.Supported == (tab.Kubectl.Offer != "") {
		t.Errorf("the terminal's kubectl check against %s: %+v", s.Backend().Conn.ServerVersion(), tab.Kubectl)
	}
	if _, err := io.WriteString(term, "kubectl get pods -n cogpit-demo --no-headers -o name; echo END-OF-PODS\n"); err != nil {
		t.Fatal(err)
	}
	out := readUntil(t, term, "pod/toolbox", 60*time.Second)
	if !strings.Contains(out, "pod/toolbox") {
		t.Errorf("kubectl in the terminal did not list the kind cluster's pods:\n%s", out)
	}
}

// readUntil reads r until the output contains want or the timeout passes.
func readUntil(t *testing.T, r io.Reader, want string, timeout time.Duration) string {
	t.Helper()
	var buf bytes.Buffer
	done := make(chan struct{})
	go func() {
		defer close(done)
		p := make([]byte, 4096)
		for {
			n, err := r.Read(p)
			buf.Write(p[:n])
			if strings.Contains(buf.String(), want) || err != nil {
				return
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(timeout):
	}
	return buf.String()
}
