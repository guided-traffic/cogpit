//go:build kind

package integration_test

import (
	"bufio"
	"context"
	"io"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// workloadNamespace is the shared kind cluster's namespace of the logs, attach, restart and scale
// tests; they create it, act only inside it, and delete it.
const workloadNamespace = "cogpit-e"

// talker is a container that prints a line with its pod's name every second.
func talker(name string) corev1.Container {
	c := sleeper(name)
	c.Command = []string{"sh", "-c", `trap 'exit 0' TERM; i=0; while true; do i=$((i+1)); echo "line $i from $(hostname)"; sleep 1 & wait; done`}
	return c
}

// talkingTemplate is podTemplate with talker containers.
func talkingTemplate(app string, containers ...string) corev1.PodTemplateSpec {
	tmpl := podTemplate(app, nil)
	tmpl.Spec.Containers = nil
	for _, c := range containers {
		tmpl.Spec.Containers = append(tmpl.Spec.Containers, talker(c))
	}
	return tmpl
}

// lines collects a stream's lines while a test reads them.
type lines struct {
	mu   sync.Mutex
	all  []string
	done chan struct{}
}

func collect(r io.Reader) *lines {
	l := &lines{done: make(chan struct{})}
	go func() {
		defer close(l.done)
		sc := bufio.NewScanner(r)
		for sc.Scan() {
			l.mu.Lock()
			l.all = append(l.all, sc.Text())
			l.mu.Unlock()
		}
	}()
	return l
}

func (l *lines) snapshot() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.all...)
}

// waitLines waits until cond holds for the lines read so far.
func (l *lines) waitLines(ctx context.Context, t *testing.T, what string, cond func([]string) bool) []string {
	t.Helper()
	var got []string
	if !testkit.WaitFor(ctx, func() bool {
		got = l.snapshot()
		return cond(got)
	}) {
		if len(got) > 20 {
			got = got[len(got)-20:]
		}
		t.Fatalf("timed out waiting for %s; last lines: %q", what, got)
	}
	return got
}

// podsInLines returns the distinct pods of "<pod>/<container> …" lines whose container is c.
func podsInLines(got []string, c string) map[string]bool {
	pods := map[string]bool{}
	for _, l := range got {
		src, _, _ := strings.Cut(l, " ")
		if pod, cont, ok := strings.Cut(src, "/"); ok && cont == c {
			pods[pod] = true
		}
	}
	return pods
}

// TestKindWorkloadLogsAttachRestartScale runs the logs of controlling resources (two pods, a
// third joining after a scale-up), timestamps, the previous container of a crashing pod, attach to
// a container with stdin and a TTY, Restart of a Daemon Set and a Stateful Set, and Scale of a
// Replica Set through the engine against the shared kind cluster, in its own namespace cogpit-e.
// It never touches the node.
func TestKindWorkloadLogsAttachRestartScale(t *testing.T) {
	kubeconfig, contextName := os.Getenv("COGPIT_KIND_KUBECONFIG"), os.Getenv("COGPIT_KIND_CONTEXT")
	if kubeconfig == "" || contextName == "" {
		t.Skip("COGPIT_KIND_KUBECONFIG and COGPIT_KIND_CONTEXT are not set; run make test-kind")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	conn, err := cluster.Connect(ctx, cluster.Options{KubeconfigPaths: []string{kubeconfig}, Context: contextName})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	kube, ns := conn.Kube(), workloadNamespace
	freshNamespace(ctx, t, kube, ns)

	selector := func(app string) *metav1.LabelSelector {
		return &metav1.LabelSelector{MatchLabels: map[string]string{"app": app}}
	}
	crash := podTemplate("crash", nil)
	crash.Spec.Containers[0].Command = []string{"sh", "-c", `echo "crash run at $(date +%s) on $(hostname)"; sleep 2; exit 1`}
	repl := podTemplate("repl", nil).Spec
	repl.Containers[0].Name, repl.Containers[0].Command = "repl", []string{"sh"}
	repl.Containers[0].Stdin, repl.Containers[0].TTY = true, true
	for _, create := range []func() error{
		func() error {
			_, err := kube.AppsV1().Deployments(ns).Create(ctx, &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: "web"},
				Spec:       appsv1.DeploymentSpec{Replicas: int32p(2), Selector: selector("web"), Template: talkingTemplate("web", "app")},
			}, metav1.CreateOptions{})
			return err
		},
		func() error {
			_, err := kube.AppsV1().Deployments(ns).Create(ctx, &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: "crash"},
				Spec:       appsv1.DeploymentSpec{Replicas: int32p(1), Selector: selector("crash"), Template: crash},
			}, metav1.CreateOptions{})
			return err
		},
		func() error {
			_, err := kube.AppsV1().DaemonSets(ns).Create(ctx, &appsv1.DaemonSet{
				ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: "agent"},
				Spec:       appsv1.DaemonSetSpec{Selector: selector("agent"), Template: talkingTemplate("agent", "agent")},
			}, metav1.CreateOptions{})
			return err
		},
		func() error {
			_, err := kube.AppsV1().StatefulSets(ns).Create(ctx, &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: "db"},
				Spec: appsv1.StatefulSetSpec{Replicas: int32p(1), ServiceName: "db", Selector: selector("db"),
					Template: talkingTemplate("db", "db", "sidecar")},
			}, metav1.CreateOptions{})
			return err
		},
		func() error {
			_, err := kube.AppsV1().ReplicaSets(ns).Create(ctx, &appsv1.ReplicaSet{
				ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: "rs"},
				Spec:       appsv1.ReplicaSetSpec{Replicas: int32p(1), Selector: selector("rs"), Template: podTemplate("rs", nil)},
			}, metav1.CreateOptions{})
			return err
		},
		func() error {
			_, err := kube.CoreV1().Pods(ns).Create(ctx, &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: "repl"}, Spec: repl}, metav1.CreateOptions{})
			return err
		},
	} {
		if err := create(); err != nil {
			t.Fatal(err)
		}
	}

	confirmer := &recordingConfirmer{}
	s := engine.New(engine.Options{Connector: &engine.ClusterConnector{KubeconfigPaths: []string{kubeconfig}}, Confirmer: confirmer})
	defer s.Close()
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	if err := s.Connect(ctx, contextName); err != nil {
		t.Fatal(err)
	}
	d := dispatcher{t: t, ctx: ctx, s: s}
	ref := func(kind, name string) model.ObjectRef { return model.ObjectRef{Kind: kind, Namespace: ns, Name: name} }
	for _, r := range []model.ObjectRef{ref("deployments", "web"), ref("deployments", "crash"), ref("daemonsets", "agent"), ref("statefulsets", "db"), ref("replicasets", "rs")} {
		d.cached(r, nil)
	}

	// Logs of the deployment: both pods, each line named; a third pod joins after a scale-up.
	waitFor(ctx, t, "two ready web pods", func() bool { return len(readyPods(ctx, kube, ns, "web")) == 2 })
	var tab commands.DockResult
	d.must("workload.logs", map[string]any{"ref": ref("deployments", "web")}, &tab)
	stream, err := s.OpenLogs(ctx, tab.Tab.ID)
	if err != nil {
		t.Fatal(err)
	}
	web := collect(stream)
	web.waitLines(ctx, t, "lines of both web pods", func(got []string) bool { return len(podsInLines(got, "app")) == 2 })
	d.must("deployment.scale", map[string]any{"ref": ref("deployments", "web"), "replicas": 3}, nil)
	got := web.waitLines(ctx, t, "a line of the third web pod", func(got []string) bool { return len(podsInLines(got, "app")) == 3 })
	for _, l := range got {
		if !strings.HasPrefix(l, "web-") && !strings.HasPrefix(l, engine.NoticePrefix) {
			t.Errorf("a line without its pod: %q", l)
		}
	}
	_ = stream.Close()

	// Timestamps: the stream opens with them, after the pod's name.
	d.must("logs.setOptions", map[string]any{"id": tab.Tab.ID, "timestamps": true}, nil)
	stream, err = s.OpenLogs(ctx, tab.Tab.ID)
	if err != nil {
		t.Fatal(err)
	}
	stamped := regexp.MustCompile(`^web-[a-z0-9-]+/app \d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(\.\d+)?Z line \d+ from web-`)
	collect(stream).waitLines(ctx, t, "a timestamped line", func(got []string) bool {
		for _, l := range got {
			if stamped.MatchString(l) {
				return true
			}
		}
		return false
	})
	_ = stream.Close()

	// The stateful set's pod streams both its containers.
	d.must("workload.logs", map[string]any{"ref": ref("statefulsets", "db")}, &tab)
	stream, err = s.OpenLogs(ctx, tab.Tab.ID)
	if err != nil {
		t.Fatal(err)
	}
	collect(stream).waitLines(ctx, t, "both containers of db-0", func(got []string) bool {
		return podsInLines(got, "db")["db-0"] && podsInLines(got, "sidecar")["db-0"]
	})
	_ = stream.Close()

	// The previous container of the crashing pod, in its own tab and in the deployment's.
	var crashPod string
	waitFor(ctx, t, "the crash pod to restart", func() bool {
		list, err := kube.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{LabelSelector: "app=crash"})
		if err != nil || len(list.Items) == 0 {
			return false
		}
		for _, st := range list.Items[0].Status.ContainerStatuses {
			if st.RestartCount > 0 && st.LastTerminationState.Terminated != nil {
				crashPod = list.Items[0].Name
				return true
			}
		}
		return false
	})
	d.cached(ref("pods", crashPod), func(o *unstructured.Unstructured) bool {
		for _, st := range model.NestedSlice(o, "status", "containerStatuses") {
			if model.NestedMap(st, "lastState", "terminated") != nil {
				return true
			}
		}
		return false
	})
	d.must("pod.logs", map[string]any{"ref": ref("pods", crashPod)}, &tab)
	d.must("logs.setOptions", map[string]any{"id": tab.Tab.ID, "previous": true}, nil)
	stream, err = s.OpenLogs(ctx, tab.Tab.ID)
	if err != nil {
		t.Fatal(err)
	}
	prev := collect(stream)
	select {
	case <-prev.done: // the previous instance's log is complete: the stream ends
	case <-ctx.Done():
		t.Fatal("the previous container's log did not end")
	}
	if got := prev.snapshot(); len(got) != 1 || !regexp.MustCompile(`^\d{4}-\d\d-\d\dT\S+Z crash run at `).MatchString(got[0]) { // every stream carries timestamps
		t.Errorf("previous container's log %q", got)
	}
	_ = stream.Close()
	d.must("workload.logs", map[string]any{"ref": ref("deployments", "crash")}, &tab)
	d.must("logs.setOptions", map[string]any{"id": tab.Tab.ID, "previous": true}, nil)
	stream, err = s.OpenLogs(ctx, tab.Tab.ID)
	if err != nil {
		t.Fatal(err)
	}
	prev = collect(stream)
	select {
	case <-prev.done:
	case <-ctx.Done():
		t.Fatal("the deployment's previous containers' logs did not end")
	}
	if got := prev.snapshot(); len(got) == 0 || !strings.HasPrefix(got[0], crashPod+"/crash ") || !strings.Contains(got[0], "Z crash run at ") {
		t.Errorf("the deployment's previous containers %q", got)
	}
	_ = stream.Close()

	// Attach to the shell of the repl pod, which has stdin and a TTY.
	waitFor(ctx, t, "the repl pod to run", func() bool {
		p, err := kube.CoreV1().Pods(ns).Get(ctx, "repl", metav1.GetOptions{})
		return err == nil && p.Status.Phase == corev1.PodRunning
	})
	d.cached(ref("pods", "repl"), func(o *unstructured.Unstructured) bool { return model.NestedString(o, "status", "phase") == "Running" })
	d.must("pod.attach", map[string]any{"ref": ref("pods", "repl")}, &tab)
	if tab.Tab.Type != engine.DockAttach || tab.Tab.Container != "repl" {
		t.Fatalf("attach tab %+v", tab.Tab)
	}
	sess, err := s.OpenTerminal(ctx, tab.Tab.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := sess.Resize(100, 30); err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(sess, "echo attached-$((40+2))\r"); err != nil {
		t.Fatal(err)
	}
	if out := readUntil(t, sess, "attached-42", 30*time.Second); !strings.Contains(out, "attached-42") || !strings.Contains(out, "[cogpit] attached to repl/repl") {
		t.Errorf("attach: %q", out)
	}
	_ = sess.Close()

	// Restart: the daemon set's and the stateful set's pods are replaced, after a confirmation.
	podUID := func(app string) string {
		list, err := kube.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{LabelSelector: "app=" + app})
		if err != nil || len(list.Items) != 1 || list.Items[0].DeletionTimestamp != nil {
			return ""
		}
		return string(list.Items[0].UID)
	}
	for _, tc := range []struct{ id, kind, app string }{{"daemonset.restart", "daemonsets", "agent"}, {"statefulset.restart", "statefulsets", "db"}} {
		var before string
		waitFor(ctx, t, "one "+tc.app+" pod", func() bool { before = podUID(tc.app); return before != "" })
		_, asked := confirmer.last()
		d.must(tc.id, map[string]any{"ref": ref(tc.kind, tc.app)}, nil)
		if req, n := confirmer.last(); n != asked+1 || req.Danger != engine.DangerDestructive || req.Command != tc.id {
			t.Errorf("%s: confirmation %+v", tc.id, req)
		}
		var restartedAt string
		if tc.kind == "daemonsets" {
			ds, err := kube.AppsV1().DaemonSets(ns).Get(ctx, tc.app, metav1.GetOptions{})
			if err == nil {
				restartedAt = ds.Spec.Template.Annotations[cluster.RestartedAtAnnotation]
			}
		} else {
			sts, err := kube.AppsV1().StatefulSets(ns).Get(ctx, tc.app, metav1.GetOptions{})
			if err == nil {
				restartedAt = sts.Spec.Template.Annotations[cluster.RestartedAtAnnotation]
			}
		}
		if _, err := time.Parse(time.RFC3339, restartedAt); err != nil {
			t.Errorf("%s: restartedAt %q", tc.id, restartedAt)
		}
		waitFor(ctx, t, "a new "+tc.app+" pod", func() bool { now := podUID(tc.app); return now != "" && now != before })
	}

	// Scale of a replica set: through its scale subresource.
	d.must("replicaset.scale", map[string]any{"ref": ref("replicasets", "rs"), "replicas": 2}, nil)
	rs, err := kube.AppsV1().ReplicaSets(ns).Get(ctx, "rs", metav1.GetOptions{})
	if err != nil || rs.Spec.Replicas == nil || *rs.Spec.Replicas != 2 {
		t.Errorf("replica set after scale: %v %v", rs.Spec.Replicas, err)
	}
}
