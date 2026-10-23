package fyneui

import (
	"context"
	"encoding/json"
	"slices"
	"sync"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// sourceContexts are the section's contexts with kubeconfig sources: a file that needs no program, a file
// whose login program is not allowed yet, and a synced one whose program changed since.
func sourceContexts() []engine.ContextInfo {
	return append(fixtureContexts()[1:3],
		engine.ContextInfo{Name: "kind-cogpit [kind.yaml]", KubeContext: "kind-cogpit", Source: "kind.yaml", Cluster: "kind-cogpit",
			Server: "https://127.0.0.1:52341", File: "/Users/me/kind.yaml"},
		engine.ContextInfo{Name: "prod-eks [eks.yaml]", KubeContext: "prod-eks", Source: "eks.yaml", Cluster: "arn:aws:eks:eu-central-1:000000000000:cluster/prod",
			Server: "https://ABCDEF.gr7.eu-central-1.eks.amazonaws.com", File: "/Users/me/Downloads/eks.yaml", Exec: engine.ExecRequired},
		engine.ContextInfo{Name: "team [kubeconfigs/team.yaml]", KubeContext: "team", Source: "kubeconfigs/team.yaml", Cluster: "team",
			Server: "https://team.example:6443", Namespace: "team-a", File: "/Users/me/kubeconfigs/team.yaml", Exec: engine.ExecChanged,
			ConnectProblem: true},
	)
}

// eksContext is one context of an approval group as kubeconfig.pendingApprovals returns it.
func eksContext(name, state, fingerprint string) map[string]any {
	return map[string]any{
		"context": name + " [eks.yaml]", "kubeContext": name, "state": state, "fingerprint": fingerprint,
		"server": "https://" + name + ".gr7.eu-central-1.eks.amazonaws.com", "tls": map[string]any{}, "files": map[string]any{},
	}
}

// pendingFixture is what kubeconfig.pendingApprovals returns for the EKS context of sourceContexts:
// one group of one context. Tests add contexts to groups[0]["contexts"].
func pendingFixture(state string) map[string]any {
	ctx := eksContext("prod-eks", state, "sha256:feed")
	ctx["server"] = "https://ABCDEF.gr7.eu-central-1.eks.amazonaws.com"
	return map[string]any{"groups": []any{map[string]any{
		"source": "eks.yaml", "file": "/Users/me/Downloads/eks.yaml", "kind": "program",
		"plugin": map[string]any{
			"apiVersion": "client.authentication.k8s.io/v1beta1",
			"command":    "aws", "args": []string{"--region", "eu-central-1", "eks", "get-token", "--cluster-name", "prod"},
			"envNames": []string{"AWS_PROFILE", "PATH"},
		},
		"found": "/opt/homebrew/bin/aws", "riskyEnv": []string{"PATH"},
		"contexts": []any{ctx},
	}}}
}

// group returns the first group of a pendingView.
func group(v map[string]any) map[string]any { return v["groups"].([]any)[0].(map[string]any) }

// recorder records the kubeconfig commands a test's fake engine receives.
type recorder struct {
	mu    sync.Mutex
	calls []engine.Invocation
}

func (r *recorder) add(inv engine.Invocation) {
	r.mu.Lock()
	r.calls = append(r.calls, inv)
	r.mu.Unlock()
}

func (r *recorder) ids() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.calls))
	for i, c := range r.calls {
		out[i] = c.ID
	}
	return out
}

func (r *recorder) params(id string) map[string]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, c := range r.calls {
		if c.ID == id {
			var p map[string]string
			_ = json.Unmarshal(c.Params, &p)
			return p
		}
	}
	return nil
}

// approvals returns the approvals of the first kubeconfig.approveExec call.
func (r *recorder) approvals() []approvalItem {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, c := range r.calls {
		if c.ID == "kubeconfig.approveExec" {
			var p approveParams
			_ = json.Unmarshal(c.Params, &p)
			return p.Approvals
		}
	}
	return nil
}

// kubeconfigFake is a fake engine with the source contexts and handlers for the kubeconfig
// commands; cluster.connect and ui.setNavigatorScope, the section's connect, are recorded too.
func kubeconfigFake(t *testing.T, state string) (*fakeEngine, *recorder) {
	f := newFakeEngine(t)
	f.contexts = sourceContexts()
	f.handlers["ui.setNavigatorScope"] = f.setNavScope
	rec := &recorder{}
	handle := func(id string, res func(engine.Invocation) (any, error)) {
		f.handlers[id] = func(_ context.Context, inv engine.Invocation) (any, error) { rec.add(inv); return res(inv) }
	}
	handle("kubeconfig.pendingApprovals", func(engine.Invocation) (any, error) { return pendingFixture(state), nil })
	handle("kubeconfig.approveExec", func(engine.Invocation) (any, error) { return map[string]any{"approved": []any{}}, nil })
	handle("kubeconfig.sources", func(engine.Invocation) (any, error) {
		return map[string]any{"sources": []map[string]any{
			{"id": ".kube/", "kind": "folder", "default": true, "path": "/Users/me/.kube", "files": []string{".kube/team.yaml", ".kube/.dev.kubeconfig"}, "contexts": 2, "covered": 1, "skipped": 1},
			{"id": "kind.yaml", "kind": "file", "path": "/Users/me/kind.yaml", "files": []string{"kind.yaml"}, "contexts": 1},
			{"id": "eks.yaml", "kind": "file", "path": "/Users/me/Downloads/eks.yaml", "files": []string{"eks.yaml"}, "contexts": 1},
			{"id": "kubeconfigs/", "kind": "folder", "path": "/Users/me/kubeconfigs", "files": []string{"kubeconfigs/team.yaml"}, "contexts": 1, "skipped": 2},
			{"id": "old.yaml", "kind": "file", "path": "/Users/me/old.yaml", "contexts": 0, "error": "kubeconfig file /Users/me/old.yaml does not exist"},
		}}, nil
	})
	handle("kubeconfig.remove", func(engine.Invocation) (any, error) { return map[string]any{"id": "eks.yaml"}, nil })
	handle("kubeconfig.addFile", func(engine.Invocation) (any, error) {
		return map[string]any{"source": map[string]any{"id": "dev.yaml", "kind": "file"}, "contexts": []string{"dev [dev.yaml]"}}, nil
	})
	return f, rec
}

// connects are the contexts the window connected, in order: cluster.connect's (the welcome page, the
// hotbar, a --context) and ui.setNavigatorScope's into a cluster (the navigator's section, D-57).
func (f *fakeEngine) connects() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, c := range f.calls {
		switch c.ID {
		case "cluster.connect":
			var p contextParams
			_ = json.Unmarshal(c.Params, &p)
			out = append(out, p.Context)
		case "ui.setNavigatorScope":
			var p navScopeParams
			_ = json.Unmarshal(c.Params, &p)
			if id, err := engine.ParseNavScope(p.Scope); err == nil && id != "" {
				out = append(out, id)
			}
		}
	}
	return out
}

// findButton returns the visible button labelled label in the window's top overlay (a dialog), or
// nil.
func findButton(w fyne.Window, label string) *widget.Button {
	overlay := w.Canvas().Overlays().Top()
	if overlay == nil {
		return nil
	}
	var found *widget.Button
	var walk func(o fyne.CanvasObject)
	walk = func(o fyne.CanvasObject) {
		if found != nil || o == nil || !o.Visible() {
			return
		}
		switch x := o.(type) {
		case *widget.Button:
			if x.Text == label {
				found = x
			}
		case *textButton:
			if x.Text == label {
				found = &x.Button
			}
		case *fyne.Container:
			for _, c := range x.Objects {
				walk(c)
			}
		case fyne.Widget:
			for _, c := range test.WidgetRenderer(x).Objects() {
				walk(c)
			}
		}
	}
	walk(overlay)
	return found
}

// selectAndConnect connects a context from the navigator's section, as a click on its row does (the
// context picker went, D-57): a login waiting for the user's approval is shown first.
func selectAndConnect(t *testing.T, sh *shell, q *mainQueue, name string) {
	t.Helper()
	n := sh.side.sec
	q.until(t, "the section's "+name, func() bool { return n.Visible() && n.loaded && n.rows[name] != nil })
	test.Tap(n.rows[name])
}

// TestSectionShowsTheLoginProgramBeforeItRuns: connecting to a context whose login program the user
// has not allowed opens the approval dialog first; nothing connects until the user allows it, and
// allowing sends the fingerprint the dialog showed, then connects.
func TestSectionShowsTheLoginProgramBeforeItRuns(t *testing.T) {
	f, rec := kubeconfigFake(t, engine.ExecRequired)
	sh, _, q := newTestShell(t, f, Options{})
	selectAndConnect(t, sh, q, "prod-eks [eks.yaml]")
	q.until(t, "the approval dialog", func() bool { return sh.kc.approval != nil })
	if got := f.connects(); len(got) != 0 {
		t.Fatalf("connected before the program was allowed: %v", got)
	}
	test.Tap(sh.kc.approval.allow)
	q.until(t, "the connection", func() bool { return len(f.connects()) == 1 })
	if p := rec.approvals(); len(p) != 1 || p[0].Context != "prod-eks [eks.yaml]" || p[0].Fingerprint != "sha256:feed" {
		t.Errorf("approveExec %v", p)
	}
	if got := f.connects(); got[0] != "prod-eks [eks.yaml]" {
		t.Errorf("connected %v", got)
	}
}

// TestCancelledApprovalDoesNotConnect: cancelling connects nothing and leaves the section shown.
func TestCancelledApprovalDoesNotConnect(t *testing.T) {
	f, rec := kubeconfigFake(t, engine.ExecChanged)
	sh, _, q := newTestShell(t, f, Options{})
	selectAndConnect(t, sh, q, "prod-eks [eks.yaml]")
	q.until(t, "the approval dialog", func() bool { return sh.kc.approval != nil })
	test.Tap(sh.kc.approval.cancel)
	q.until(t, "the dialog closed", func() bool { return sh.kc.approval == nil })
	q.drain()
	if slices.Contains(rec.ids(), "kubeconfig.approveExec") || len(f.connects()) != 0 || !sh.side.sec.Visible() {
		t.Errorf("calls %v, connects %v, section %v", rec.ids(), f.connects(), sh.side.sec.Visible())
	}
}

// TestSectionConnectsContextsWithoutAProgramDirectly: no dialog for a context without a program to
// allow.
func TestSectionConnectsContextsWithoutAProgramDirectly(t *testing.T) {
	f, rec := kubeconfigFake(t, engine.ExecRequired)
	sh, _, q := newTestShell(t, f, Options{})
	selectAndConnect(t, sh, q, "kind-cogpit [kind.yaml]")
	q.until(t, "the connection", func() bool { return len(f.connects()) == 1 })
	if sh.kc.approval != nil || slices.Contains(rec.ids(), "kubeconfig.pendingApprovals") {
		t.Errorf("asked about a context without a program: %v", rec.ids())
	}
}

// TestSourcesDialogRemovesAfterConfirming: Remove asks, then dispatches kubeconfig.remove.
func TestSourcesDialogRemovesAfterConfirming(t *testing.T) {
	f, rec := kubeconfigFake(t, engine.ExecRequired)
	sh, w, q := newTestShell(t, f, Options{})
	sh.kc.showSources()
	sd := sh.kc.sources
	q.until(t, "the sources", func() bool { return len(sd.rows) == 5 })
	test.Tap(sd.removeB["eks.yaml"])
	confirm := findButton(w, "Remove")
	if confirm == nil || sd.removing == nil {
		t.Fatal("no confirmation before removing")
	}
	if slices.Contains(rec.ids(), "kubeconfig.remove") {
		t.Fatal("removed before the confirmation")
	}
	test.Tap(confirm)
	q.until(t, "kubeconfig.remove", func() bool { return slices.Contains(rec.ids(), "kubeconfig.remove") })
	if p := rec.params("kubeconfig.remove"); p["source"] != "eks.yaml" {
		t.Errorf("remove %v", p)
	}
}

// TestSyncFromTheMenuAndEventsReloadTheSection: the Cluster menu offers one entry for files and
// folders (the old two are gone); a path the dialog hands back goes to kubeconfig.addFile or
// kubeconfig.addFolder by what it is; EventContexts makes the navigator's section list again.
func TestSyncFromTheMenuAndEventsReloadTheSection(t *testing.T) {
	f, rec := kubeconfigFake(t, engine.ExecRequired)
	f.handlers["kubeconfig.addFolder"] = func(_ context.Context, inv engine.Invocation) (any, error) {
		rec.add(inv)
		return map[string]any{"source": map[string]any{"id": "team/", "kind": "folder"}, "contexts": []string{}}, nil
	}
	sh, _, q := newTestShell(t, f, Options{})
	n := sh.side.sec
	q.until(t, "the section", func() bool { return n.loaded && len(n.contexts) == len(f.contexts) })
	var labels []string
	for _, it := range sh.mainMenu().Items[0].Items {
		labels = append(labels, it.Label)
	}
	for _, want := range []string{"Sync Files and Folders…", "Kubeconfig Sources…"} {
		if !slices.Contains(labels, want) {
			t.Errorf("Cluster menu %v lacks %q", labels, want)
		}
	}
	for _, gone := range []string{"Add Kubeconfig File…", "Add Sync Folder…"} {
		if slices.Contains(labels, gone) {
			t.Errorf("Cluster menu still has %q", gone)
		}
	}
	var asked int
	sh.kc.pick = func(done func(string, bool)) {
		asked++
		if asked == 1 {
			done("/Users/me/dev.yaml", false)
		} else {
			done("/Users/me/team", true)
		}
	}
	sh.kc.addKubeconfigs()
	q.until(t, "kubeconfig.addFile", func() bool { return slices.Contains(rec.ids(), "kubeconfig.addFile") })
	sh.kc.addKubeconfigs()
	q.until(t, "kubeconfig.addFolder", func() bool { return slices.Contains(rec.ids(), "kubeconfig.addFolder") })
	if p := rec.params("kubeconfig.addFile"); p["path"] != "/Users/me/dev.yaml" {
		t.Errorf("addFile %v", p)
	}
	if p := rec.params("kubeconfig.addFolder"); p["path"] != "/Users/me/team" || asked != 2 {
		t.Errorf("addFolder %v, asked %d", p, asked)
	}

	f.mu.Lock()
	f.contexts = append(f.contexts, engine.ContextInfo{Name: "dev [dev.yaml]", KubeContext: "dev", Source: "dev.yaml"})
	f.mu.Unlock()
	f.emit(engine.Event{Type: engine.EventContexts})
	q.until(t, "the section to list the new context", func() bool {
		return len(n.contexts) == len(sourceContexts())+1 && n.rows["dev [dev.yaml]"] != nil
	})
}

func renderApproval(t *testing.T, mode ThemeMode, state, name string) {
	f, _ := kubeconfigFake(t, state)
	sh, w, q := newTestShell(t, f, Options{Theme: mode})
	selectAndConnect(t, sh, q, "prod-eks [eks.yaml]")
	q.until(t, "the approval dialog", func() bool { return sh.kc.approval != nil })
	savePNG(t, w, name)
}

func TestRenderExecApprovalDark(t *testing.T) {
	renderApproval(t, ThemeDark, engine.ExecRequired, "exec_approval_dark.png")
}

func TestRenderExecApprovalLight(t *testing.T) {
	renderApproval(t, ThemeLight, engine.ExecChanged, "exec_approval_changed_light.png")
}

func TestRenderKubeconfigSources(t *testing.T) {
	f, _ := kubeconfigFake(t, engine.ExecRequired)
	sh, w, q := newTestShell(t, f, Options{})
	sh.kc.showSources()
	q.until(t, "the sources", func() bool { return sh.kc.sources != nil && len(sh.kc.sources.rows) == 5 })
	savePNG(t, w, "kubeconfig_sources_dark.png")
}
