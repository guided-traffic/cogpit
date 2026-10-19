//go:build kind

package integration_test

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// TestRealDebugContainer checks debug containers (D-102) on a real cluster (make test-real,
// REAL_RUN=TestRealDebugContainer), where kind cannot: the cluster's own image pulls, its
// runtime and, when it runs Kyverno, an admission webhook's refusal. In a namespace of its own
// (cogpit-real-debug, deleted at the end) it adds a debug container to a running pod through the
// engine, attaches to it, sees the target's process, ends it, and then reads what the window
// shows from the cache: the Pods list's squares (one regular, one finished ephemeral outlined
// blue) and the drawer's Ephemeral Containers section with its Target. When the cluster serves
// Kyverno's Policies, a namespaced Policy in that namespace (admission only, Enforce, matching
// Pod/ephemeralcontainers) must make the next Debug fail with Kyverno's message and add nothing.
// COGPIT_REAL_DEBUG_POD=<namespace>/<name> also checks a pod debugged by hand (kubectl debug):
// every ephemeral container of it has its Target row and its square.
func TestRealDebugContainer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	s, confirm, _ := realSession(ctx, t, engine.Options{Preferences: engine.NewPreferencesStore("")})
	if p := os.Getenv("COGPIT_REAL_DEBUG_POD"); p != "" {
		ns, name, _ := strings.Cut(p, "/")
		checkDebuggedPod(ctx, t, s, model.ObjectRef{Kind: "pods", Namespace: ns, Name: name})
	}

	ns := realNamespace(ctx, t, s, "debug")
	b := s.Backend()
	kube := b.Conn.Kube()
	// The ticker without its pod-wide runAsNonRoot, which would apply to the debug container too and
	// keep netshoot (root) from starting.
	pod := ticker(ns, "target")
	pod.Spec.SecurityContext = nil
	if _, err := kube.CoreV1().Pods(ns).Create(ctx, pod, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := b.Conn.WaitPodRunning(ctx, ns, "target", "tick"); err != nil {
		t.Fatalf("the target pod: %v", err)
	}
	if _, err := s.Objects(ctx, "pods", nil); err != nil { // the Pods view's cache, which Enabled reads
		t.Fatal(err)
	}
	ref := model.ObjectRef{Kind: "pods", Namespace: ns, Name: "target"}
	waitFor(ctx, t, "the target in the cache", func() bool { o, err := s.Lookup(ref); return err == nil && o != nil && engine.PodEnded(o) == "" })

	start := time.Now()
	raw, _ := json.Marshal(map[string]any{"ref": ref, "target": "tick"})
	res, err := s.Dispatch(ctx, engine.Invocation{ID: engine.DebugCommand, Params: raw, Origin: engine.OriginGUI})
	if err != nil {
		t.Fatalf("pod.debug: %v", err)
	}
	debug := res.(commands.DebugResult)
	if req, n := confirm.last(); n != 1 || req.Danger != engine.DangerDestructive || !strings.Contains(req.Message(), "Target: tick.") {
		t.Errorf("confirmation (%d): %+v", n, req)
	}
	if debug.Tab == nil || debug.Image != engine.DefaultDebugImage {
		t.Fatalf("result %+v", debug)
	}
	sess, err := s.OpenTerminal(ctx, debug.Tab.ID)
	if err != nil {
		t.Fatalf("OpenTerminal: %v", err)
	}
	out := collectShell(sess)
	waitFor(ctx, t, "the debug shell", func() bool { return strings.Contains(out.String(), "[cogpit] attached to target/"+debug.Container) })
	t.Logf("attached %s after the debug's dispatch (the image pulled on the cluster's node)", time.Since(start).Round(time.Second))
	answered := func(want string) bool {
		wctx, wcancel := context.WithTimeout(ctx, 10*time.Second)
		defer wcancel()
		return testkit.WaitFor(wctx, func() bool { return strings.Contains(out.String(), want) })
	}
	for i := 0; ; i++ {
		if _, err := io.WriteString(sess, "echo REAL-$((40+2))-OK PROC-$(ps -o args | grep -c '[t]ick-')-END\r"); err != nil {
			t.Fatal(err)
		}
		if answered("REAL-42-OK PROC-1-END") {
			break
		}
		if i == 5 {
			t.Fatalf("the debug shell did not answer, or does not see the target's process:\n%q", out.String())
		}
	}
	if _, err := io.WriteString(sess, "exit\r"); err != nil {
		t.Fatal(err)
	}

	// What the window shows, from the cache once the watch brings the finished container: the
	// Pods list's squares and the drawer's section with its Target. The tab stays open until then:
	// closing it first may drop the exit, and the shell would run on (an attach ends no container).
	waitFor(ctx, t, "the debug container finished in the cache", func() bool {
		o, err := s.Lookup(ref)
		if err != nil || o == nil {
			return false
		}
		for _, st := range model.NestedSlice(o.Object, "status", "ephemeralContainerStatuses") {
			if model.NestedString(st, "name") == debug.Container && model.NestedMap(st, "state", "terminated") != nil {
				return true
			}
		}
		return false
	})
	_ = sess.Close()
	marks := podMarks(ctx, t, s, ref)
	want := []model.CellMark{
		{Text: "tick: running", Status: model.StatusOK},
		{Text: debug.Container + ": ephemeral, terminated (Completed, exit code 0), target tick", Status: model.StatusEphemeral, Hollow: true},
	}
	if !slices.Equal(marks, want) {
		t.Errorf("the Pods list's squares %+v, want %+v", marks, want)
	}
	checkDebuggedPod(ctx, t, s, ref)

	// Kyverno's admission webhook, when the cluster runs it: a Policy of the test's namespace that
	// refuses every ephemeral container.
	if _, ok := b.Profile("policies.kyverno.io"); !ok {
		t.Log("the cluster serves no Kyverno Policies: no webhook refusal checked")
		return
	}
	policies := schema.GroupVersionResource{Group: "kyverno.io", Version: "v1", Resource: "policies"}
	const refusal = "No debug containers in this namespace (a Cogpit test)." // no apostrophe: Kyverno quotes the message as YAML
	policy := testkit.Object("kyverno.io/v1", "Policy", ns, "cogpit-no-debug", map[string]any{"spec": map[string]any{
		"admission": true, "background": false,
		"rules": []any{map[string]any{
			"name":  "no-ephemeral-containers",
			"match": map[string]any{"any": []any{map[string]any{"resources": map[string]any{"kinds": []any{"Pod/ephemeralcontainers"}}}}},
			"validate": map[string]any{
				"failureAction": "Enforce",
				"message":       refusal,
				"deny":          map[string]any{},
			},
		}},
	}})
	policy.SetUID("")
	policy.SetCreationTimestamp(metav1.Time{})
	if _, err := b.Conn.Dynamic().Resource(policies).Namespace(ns).Create(ctx, policy, metav1.CreateOptions{}); err != nil {
		t.Fatalf("the Kyverno Policy: %v", err)
	}
	waitFor(ctx, t, "the Kyverno Policy ready", func() bool {
		p, err := b.Conn.Dynamic().Resource(policies).Namespace(ns).Get(ctx, "cogpit-no-debug", metav1.GetOptions{})
		if err != nil {
			return false
		}
		for _, c := range model.NestedSlice(p.Object, "status", "conditions") {
			if model.NestedString(c, "type") == "Ready" && model.NestedString(c, "status") == "True" {
				return true
			}
		}
		return false
	})
	// Kyverno registers its webhook for the new kind a moment after the Policy is ready: a Debug that
	// still gets through adds a container, which the count below allows for.
	var refused error
	added := 0
	for i := 0; i < 12 && refused == nil; i++ {
		_, err := s.Dispatch(ctx, engine.Invocation{ID: engine.DebugCommand, Params: raw, Origin: engine.OriginGUI})
		switch {
		case err != nil:
			refused = err
		default:
			added++
			time.Sleep(5 * time.Second)
		}
	}
	t.Logf("Kyverno's refusal after %d debug containers got through: %v", added, refused)
	if refused == nil || !strings.Contains(refused.Error(), refusal) {
		t.Fatalf("under the Policy, Debug: %v", refused)
	}
	p, err := kube.CoreV1().Pods(ns).Get(ctx, "target", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if n := len(p.Spec.EphemeralContainers); n != 1+added {
		t.Errorf("%d ephemeral containers after the refusal, want %d", n, 1+added)
	}
}

// podMarks are the squares of ref's row in the Pods list, once the list has it.
func podMarks(ctx context.Context, t *testing.T, s *engine.Session, ref model.ObjectRef) []model.CellMark {
	t.Helper()
	var marks []model.CellMark
	waitFor(ctx, t, "the pod's row in the Pods list", func() bool {
		ts := s.Table("pods")
		if ts == nil || !ts.Synced {
			return false
		}
		ri := slices.IndexFunc(ts.Rows, func(r model.Row) bool { return r.Ref.Namespace == ref.Namespace && r.Ref.Name == ref.Name })
		if ri < 0 {
			return false
		}
		for _, c := range ts.Rows[ri].Cells { // the Containers cell is the one with squares
			if c.Marks != nil {
				marks = *c.Marks
				return true
			}
		}
		return false
	})
	return marks
}

// checkDebuggedPod: every ephemeral container of the pod has its section under Ephemeral
// Containers with its Target (or none), and a square named ephemeral in the Pods list.
func checkDebuggedPod(ctx context.Context, t *testing.T, s *engine.Session, ref model.ObjectRef) {
	t.Helper()
	if _, err := s.Objects(ctx, "pods", []string{ref.Namespace}); err != nil {
		t.Fatal(err)
	}
	var obj *unstructured.Unstructured
	waitFor(ctx, t, ref.Namespace+"/"+ref.Name+" in the cache", func() bool { o, err := s.Lookup(ref); obj = o; return err == nil && o != nil })
	d, err := s.DetailWait(ctx, ref)
	if err != nil {
		t.Fatal(err)
	}
	marks := podMarks(ctx, t, s, ref)
	specs := model.NestedSlice(obj.Object, "spec", "ephemeralContainers")
	if len(specs) == 0 {
		t.Errorf("%s has no ephemeral containers", ref.Name)
	}
	for _, spec := range specs {
		name, target := model.NestedString(spec, "name"), model.NestedString(spec, "targetContainerName")
		i := slices.IndexFunc(d.Sections, func(sec model.DetailSection) bool { return sec.Group == "Ephemeral Containers" && sec.Title == name })
		if i < 0 {
			t.Errorf("%s: no drawer section for %s", ref.Name, name)
			continue
		}
		got := ""
		for _, it := range d.Sections[i].Items {
			if it.Name == "Target" {
				got = it.Value
			}
		}
		if got != target {
			t.Errorf("%s/%s: the drawer's Target %q, the spec's %q", ref.Name, name, got, target)
		}
		if !slices.ContainsFunc(marks, func(m model.CellMark) bool {
			return strings.HasPrefix(m.Text, name+": ephemeral, ") && m.Status != "" && (target == "" || strings.HasSuffix(m.Text, ", target "+target))
		}) {
			t.Errorf("%s/%s: no square %+v", ref.Name, name, marks)
		}
		t.Logf("%s/%s: Target %q, square ok", ref.Name, name, got)
	}
}

