//go:build kind

package integration_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"

	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/mcp"
	"github.com/guided-traffic/cogpit/internal/model"
)

// mcpFlowNamespace is the namespace of TestKindAgentTriageAndChangeOverMCP; it creates it, acts
// only inside it, and deletes it.
const mcpFlowNamespace = "cogpit-mcpflow"

// streamingWindow stands in for the window's part of dock.read (engine.DockOutputReader): as the
// window's logs view does, it streams each logs tab with Session.OpenLogs, again when the tab's
// options change, and pages the lines it holds. It shows no terminal tab.
type streamingWindow struct {
	ctx context.Context
	s   *engine.Session

	mu   sync.Mutex
	tabs map[string]*streamedTab
}

type streamedTab struct {
	options string // the tab's options the stream was opened with
	rc      io.ReadCloser
	lines   *lines
}

type heldLines []string

func (h heldLines) Len() int           { return len(h) }
func (h heldLines) Number(i int) int64 { return int64(i + 1) }
func (h heldLines) Text(i int) string  { return h[i] }
func (w *streamingWindow) close()      { w.each(func(st *streamedTab) { _ = st.rc.Close() }) }
func (w *streamingWindow) each(f func(*streamedTab)) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, st := range w.tabs {
		f(st)
	}
}

func (w *streamingWindow) ReadDockOutput(_ context.Context, tab engine.DockTab, q engine.DockOutputQuery) (engine.DockOutput, error) {
	if tab.Type != engine.DockLogs {
		return engine.DockOutput{}, engine.ErrNoDockWindow
	}
	opts, _ := json.Marshal(tab.Logs)
	w.mu.Lock()
	st := w.tabs[tab.ID]
	if st == nil || st.options != string(opts) {
		if st != nil {
			_ = st.rc.Close()
		}
		rc, err := w.s.OpenLogs(w.ctx, tab.ID)
		if err != nil {
			w.mu.Unlock()
			return engine.DockOutput{}, err
		}
		st = &streamedTab{options: string(opts), rc: rc, lines: collect(rc)}
		w.tabs[tab.ID] = st
	}
	w.mu.Unlock()
	held := heldLines(st.lines.snapshot())
	return engine.DockOutput{Tab: tab, DockPage: engine.PageDockLines(held, q), Shown: len(held), Held: len(held)}, nil
}

// agentOverHTTP starts Cogpit's MCP server on s and connects an MCP client to it over loopback
// HTTP with the server's bearer token, as an external agent does.
func agentOverHTTP(ctx context.Context, t *testing.T, s *engine.Session) *sdk.ClientSession {
	t.Helper()
	tokenDir := t.TempDir()
	if err := os.Chmod(tokenDir, 0o700); err != nil { // the server refuses a directory others can read
		t.Fatal(err)
	}
	srv, err := mcp.New(s, mcp.Options{TokenFile: filepath.Join(tokenDir, "mcp-token")})
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	token, err := os.ReadFile(srv.TokenPath())
	if err != nil {
		t.Fatal(err)
	}
	tr := &http.Transport{}
	t.Cleanup(tr.CloseIdleConnections)
	cs, err := sdk.NewClient(&sdk.Implementation{Name: "workflows", Version: "1"}, nil).Connect(ctx,
		&sdk.StreamableClientTransport{
			Endpoint:   srv.URL(),
			HTTPClient: &http.Client{Transport: bearer{token: strings.TrimSpace(string(token)), base: tr}},
			MaxRetries: -1,
		}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

// toolError calls a tool that must fail and returns its text.
func toolError(ctx context.Context, t *testing.T, cs *sdk.ClientSession, name string, args map[string]any) string {
	t.Helper()
	res, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	var text string
	for _, c := range res.Content {
		if tc, ok := c.(*sdk.TextContent); ok {
			text += tc.Text
		}
	}
	if !res.IsError {
		t.Fatalf("%s succeeded, want an error: %s", name, text)
	}
	return text
}

// TestKindAgentTriageAndChangeOverMCP is M9's measure end to end (docs/parity/roadmap.md): an
// external agent, an MCP client over loopback HTTP with the bearer token, completes the alpha
// gate's triage and change workflows on the shared kind cluster through Cogpit's tools alone, and
// every mutating step waits for the user's acceptance in the window (the GUI's Confirmer, here one
// that accepts and records). Triage: the Pods list with a crashing pod's status, the pod shown to
// the user in its drawer, its containers, conditions and events, the previous container's log
// read from its logs tab, and a shell in a container, opened only once the user accepted and not
// readable by the agent without the user's switch (D-44). Change: an edit applied after a dry run,
// a Deployment scaled and restarted, a pod deleted. Each confirmation names the command, the
// object, its namespace and the context; the audit log holds every call as the agent's.
func TestKindAgentTriageAndChangeOverMCP(t *testing.T) {
	kubeconfig, contextName := os.Getenv("COGPIT_KIND_KUBECONFIG"), os.Getenv("COGPIT_KIND_CONTEXT")
	if kubeconfig == "" || contextName == "" {
		t.Skip("COGPIT_KIND_KUBECONFIG and COGPIT_KIND_CONTEXT are not set; run make test-kind")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	conn, err := cluster.Connect(ctx, cluster.Options{KubeconfigPaths: []string{kubeconfig}, Context: contextName})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	kube, ns := conn.Kube(), mcpFlowNamespace
	freshNamespace(ctx, t, kube, ns)

	crash := podTemplate("crash", nil)
	crash.Spec.Containers[0].Command = []string{"sh", "-c", `echo "crash run at $(date +%s)"; sleep 2; exit 1`}
	for name, tmpl := range map[string]corev1.PodTemplateSpec{"crash": crash, "web": podTemplate("web", nil)} {
		if _, err := kube.AppsV1().Deployments(ns).Create(ctx, &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name},
			Spec: appsv1.DeploymentSpec{Replicas: int32p(1), Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": name}},
				Template: tmpl},
		}, metav1.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
	}

	confirmer := &recordingConfirmer{}
	audit := &syncBuffer{}
	s := engine.New(engine.Options{
		Connector: &engine.ClusterConnector{KubeconfigPaths: []string{kubeconfig}}, Confirmer: confirmer, Audit: audit,
	})
	defer s.Close()
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	window := &streamingWindow{ctx: ctx, s: s, tabs: map[string]*streamedTab{}}
	defer window.close()
	s.SetDockOutputReader(window)
	if err := s.Connect(ctx, contextName); err != nil {
		t.Fatal(err)
	}
	cs := agentOverHTTP(ctx, t, s)
	d := dispatcher{t: t, ctx: ctx, s: s}
	confirmed := func(command string, ref model.ObjectRef) {
		t.Helper()
		req, n := confirmer.last()
		if n == 0 || req.Command != command || req.Origin != engine.OriginExternalAgent || req.Context != contextName ||
			!slices.Equal(req.Namespaces, []string{ns}) || len(req.Objects) != 1 || req.Objects[0].Name != ref.Name || req.Objects[0].Kind != ref.Kind {
			t.Fatalf("after %s the last confirmation (of %d) is %+v", command, n, req)
		}
	}
	asked := func() int { _, n := confirmer.last(); return n }

	// Triage: the crashing pod in the Pods list, once it restarted.
	var crashPod model.ObjectRef
	waitFor(ctx, t, "the crash pod to restart with a previous container", func() bool {
		var list commands.ListResult
		callTool(ctx, t, cs, "resources_list", map[string]any{"kind": "pods", "namespaces": []string{ns}, "filter": "crash-"}, &list)
		if len(list.Rows) != 1 || list.Level(0) != model.StatusError {
			return false
		}
		obj, err := s.Object(ctx, list.RefAt(0))
		if err != nil {
			return false
		}
		for _, st := range model.NestedSlice(obj, "status", "containerStatuses") {
			if model.NestedMap(st, "lastState", "terminated") != nil {
				crashPod = list.RefAt(0)
				return true
			}
		}
		return false
	})
	var nav commands.NavigateResult
	callTool(ctx, t, cs, "ui_navigate", map[string]any{"ref": crashPod}, &nav)
	if ui := s.UIState(); !nav.Found || ui.View != "pods" || ui.Drawer == nil || !ui.Drawer.Same(crashPod) {
		t.Fatalf("ui_navigate: %+v, the window's drawer %v", nav, ui.Drawer)
	}
	var detail engine.DetailModel
	waitFor(ctx, t, "the crash pod's events", func() bool {
		callTool(ctx, t, cs, "object_get", map[string]any{"ref": crashPod, "noYaml": true}, &detail)
		return len(detail.Events) > 0
	})
	// The drawer's Properties name the conditions; each container has a section of its own, titled
	// by its name under the group Containers.
	conditions, container := false, false
	for _, sec := range detail.Sections {
		container = container || sec.Title == "crash" && sec.Group == "Containers"
		if sec.Title == model.TitleProperties {
			conditions = slices.ContainsFunc(sec.Items, func(it model.DetailItem) bool { return it.Name == "Conditions" && it.Value != "" })
		}
	}
	if !slices.Equal(detail.Containers, []string{"crash"}) || !conditions || !container {
		t.Errorf("object_get: containers %v, conditions %v, a container section %v: %+v", detail.Containers, conditions, container, detail.Sections)
	}

	// The previous container's log. Right after a restart the kubelet may not hand it out yet
	// ("unable to retrieve container logs"): the agent closes the tab and opens it again, as a
	// user would.
	openPrevious := func() string {
		var tab commands.DockResult
		callTool(ctx, t, cs, "pod_logs", map[string]any{"ref": crashPod}, &tab)
		callTool(ctx, t, cs, "logs_setOptions", map[string]any{"id": tab.Tab.ID, "previous": true}, nil)
		return tab.Tab.ID
	}
	logsTab := openPrevious()
	waitFor(ctx, t, "the previous container's log in dock_read", func() bool {
		var out engine.DockOutput
		callTool(ctx, t, cs, "dock_read", map[string]any{"id": logsTab}, &out)
		if slices.ContainsFunc(out.Lines, func(l string) bool { return strings.Contains(l, "crash run at ") }) {
			return true
		}
		if slices.ContainsFunc(out.Lines, func(l string) bool { return strings.Contains(l, "unable to retrieve container logs") }) {
			callTool(ctx, t, cs, "dock_close", map[string]any{"id": logsTab}, nil)
			logsTab = openPrevious()
		}
		return false
	})

	var webPods commands.ListResult
	callTool(ctx, t, cs, "resources_list", map[string]any{"kind": "pods", "namespaces": []string{ns}, "filter": "web-"}, &webPods)
	if len(webPods.Rows) != 1 {
		t.Fatalf("resources_list web: %+v", webPods)
	}
	before := asked()
	var shell commands.DockResult
	callTool(ctx, t, cs, "pod_shell", map[string]any{"ref": webPods.RefAt(0)}, &shell)
	if asked() != before+1 || shell.Tab.Type != engine.DockShell {
		t.Fatalf("pod_shell: %+v, asked %d times", shell, asked()-before)
	}
	confirmed("pod.shell", webPods.RefAt(0))
	if msg := toolError(ctx, t, cs, "dock_read", map[string]any{"id": shell.Tab.ID}); !strings.Contains(msg, "D-44") {
		t.Errorf("dock_read of the shell before the user's switch: %q", msg)
	}

	// Change: an edit with a dry run, scale, restart, delete.
	web := model.ObjectRef{Context: contextName, Kind: "deployments", Namespace: ns, Name: "web"}
	d.cached(web, nil)
	var edit commands.EditResult
	callTool(ctx, t, cs, "object_edit", map[string]any{"ref": web}, &edit)
	var obj map[string]any
	if err := yaml.Unmarshal([]byte(edit.Text), &obj); err != nil {
		t.Fatal(err)
	}
	u := unstructured.Unstructured{Object: obj}
	labels := u.GetLabels()
	if labels == nil {
		labels = map[string]string{}
	}
	labels["cogpit.test/triaged"] = "yes"
	u.SetLabels(labels)
	edited, err := yaml.Marshal(u.Object)
	if err != nil {
		t.Fatal(err)
	}
	before = asked()
	callTool(ctx, t, cs, "object_dryRun", map[string]any{"id": edit.Tab.ID, "yaml": string(edited)}, nil)
	if asked() != before {
		t.Errorf("the dry run asked the user")
	}
	callTool(ctx, t, cs, "object_apply", map[string]any{"id": edit.Tab.ID, "yaml": string(edited)}, nil)
	confirmed("object.apply", web)
	waitFor(ctx, t, "the label on the API server", func() bool {
		dep, err := kube.AppsV1().Deployments(ns).Get(ctx, "web", metav1.GetOptions{})
		return err == nil && dep.Labels["cogpit.test/triaged"] == "yes"
	})

	callTool(ctx, t, cs, "deployment_scale", map[string]any{"ref": web, "replicas": 2}, nil)
	confirmed("deployment.scale", web)
	waitFor(ctx, t, "two replicas on the API server", func() bool {
		dep, err := kube.AppsV1().Deployments(ns).Get(ctx, "web", metav1.GetOptions{})
		return err == nil && dep.Spec.Replicas != nil && *dep.Spec.Replicas == 2
	})
	d.cached(web, func(o *unstructured.Unstructured) bool {
		n, _, _ := unstructured.NestedInt64(o.Object, "spec", "replicas")
		return n == 2
	})
	callTool(ctx, t, cs, "deployment_restart", map[string]any{"ref": web}, nil)
	confirmed("deployment.restart", web)
	waitFor(ctx, t, "the restart annotation on the API server", func() bool {
		dep, err := kube.AppsV1().Deployments(ns).Get(ctx, "web", metav1.GetOptions{})
		return err == nil && dep.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"] != ""
	})

	gone, err := kube.CoreV1().Pods(ns).Get(ctx, crashPod.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	callTool(ctx, t, cs, "object_delete", map[string]any{"ref": crashPod}, nil)
	confirmed("object.delete", crashPod)
	waitFor(ctx, t, "the deleted pod gone or replaced", func() bool {
		p, err := kube.CoreV1().Pods(ns).Get(ctx, crashPod.Name, metav1.GetOptions{})
		return err != nil || p.UID != gone.UID || p.DeletionTimestamp != nil
	})

	if n := asked(); n != 5 {
		t.Errorf("the user was asked %d times, want 5 (shell, apply, scale, restart, delete)", n)
	}
	// The audit log: every call is the agent's; each write has its requested and done lines.
	writes := map[string]int{}
	for _, line := range strings.Split(strings.TrimSpace(audit.String()), "\n") {
		var e engine.AuditEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatalf("audit line %q: %v", line, err)
		}
		if e.Origin != engine.OriginExternalAgent && e.Origin != engine.OriginGUI {
			t.Errorf("audit entry of %s from %s", e.Command, e.Origin)
		}
		if e.Origin == engine.OriginExternalAgent && e.Phase != "" {
			writes[e.Command+" "+e.Phase]++
		}
	}
	for _, c := range []string{"pod.shell", "object.apply", "deployment.scale", "deployment.restart", "object.delete"} {
		if writes[c+" requested"] != 1 || writes[c+" done"] != 1 {
			t.Errorf("audit of %s: %d requested, %d done lines", c, writes[c+" requested"], writes[c+" done"])
		}
	}
}
