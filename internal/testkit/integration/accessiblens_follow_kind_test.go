//go:build kind

package integration_test

import (
	"context"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/metrics"
	"github.com/guided-traffic/cogpit/internal/model"
)

// nsLimited is the namespace of the namespace-limited user's test on the shared kind cluster: it
// creates it, acts only inside it, and deletes it with the ServiceAccount, Role and RoleBinding.
const nsLimited = "cogpit-ns-a"

// TestKindNamespaceLimitedUser connects Cogpit's engine as a ServiceAccount whose one RoleBinding
// is in cogpit-ns-a, the context's namespace, with no accessible namespaces named (G3, G12): the
// rules of that namespace allow the kinds, so the first read of each kind in all namespaces is
// refused by the real API server, and every reader follows that to cogpit-ns-a. The Pods view
// shows the Deployment's pod with its CPU and Memory (the pods' metrics listed in cogpit-ns-a: a
// list in all namespaces is refused this user), an agent's object_get of the Deployment answers at
// once, the Deployment's logs stream, metrics.get reads the pod, and no read in all namespaces is
// left in the cache.
func TestKindNamespaceLimitedUser(t *testing.T) {
	kubeconfig, contextName := os.Getenv("COGPIT_KIND_KUBECONFIG"), os.Getenv("COGPIT_KIND_CONTEXT")
	if kubeconfig == "" || contextName == "" {
		t.Skip("COGPIT_KIND_KUBECONFIG and COGPIT_KIND_CONTEXT are not set; run make test-kind")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	admin, err := cluster.Connect(ctx, cluster.Options{KubeconfigPaths: []string{kubeconfig}, Context: contextName})
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	kube := admin.Kube()
	freshNamespace(ctx, t, kube, nsLimited)

	one := int32(1)
	tpl := podTemplate("web", nil)
	tpl.Spec.Containers[0].Command = []string{"sh", "-c", "echo hello from web; trap 'exit 0' TERM; while true; do sleep 3600 & wait; done"}
	if _, err := kube.AppsV1().Deployments(nsLimited).Create(ctx, &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "web"},
		Spec: appsv1.DeploymentSpec{
			Replicas: &one, Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "web"}}, Template: tpl,
		},
	}, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	const sa = "ns-reader"
	if _, err := kube.CoreV1().ServiceAccounts(nsLimited).Create(ctx, &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: sa}}, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	read := []string{"get", "list", "watch"}
	if _, err := kube.RbacV1().Roles(nsLimited).Create(ctx, &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{Name: sa},
		Rules: []rbacv1.PolicyRule{
			{Verbs: read, APIGroups: []string{""}, Resources: []string{"pods", "events"}},
			{Verbs: []string{"get"}, APIGroups: []string{""}, Resources: []string{"pods/log"}},
			{Verbs: read, APIGroups: []string{"apps"}, Resources: []string{"deployments", "replicasets"}},
			{Verbs: read, APIGroups: []string{"metrics.k8s.io"}, Resources: []string{"pods"}},
		},
	}, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := kube.RbacV1().RoleBindings(nsLimited).Create(ctx, &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: sa},
		Subjects:   []rbacv1.Subject{{Kind: rbacv1.ServiceAccountKind, Name: sa, Namespace: nsLimited}},
		RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: sa},
	}, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	expiry := int64(900)
	tr, err := kube.CoreV1().ServiceAccounts(nsLimited).CreateToken(ctx, sa, &authenticationv1.TokenRequest{
		Spec: authenticationv1.TokenRequestSpec{ExpirationSeconds: &expiry},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// The token lives only in this temporary kubeconfig; it is never logged.
	path := writeTokenKubeconfig(t, admin, sa, nsLimited, tr.Status.Token)
	waitFor(ctx, t, "the web pod ready", func() bool { return len(readyPods(ctx, kube, nsLimited, "web")) == 1 })

	s := engine.New(engine.Options{
		Connector: &engine.ClusterConnector{KubeconfigPaths: []string{path},
			MetricsOptions: metrics.Options{ListInterval: 5 * time.Second, TTL: 5 * time.Second}},
		ClusterSettings: engine.NewClusterSettingsStore(t.TempDir()),
	})
	defer s.Close()
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	decideIntegrations(t, s, sa) // its metrics read (D-82)
	if err := s.Connect(ctx, sa); err != nil {
		t.Fatal(err)
	}

	// The Pods view: the refused read in all namespaces followed to cogpit-ns-a, the pod's CPU from
	// the pods' metrics listed there (the metrics-server measures a new pod within a minute or so).
	var ts *engine.TableSnapshot
	waitFor(ctx, t, "the Pods view with the web pod's CPU", func() bool {
		ts = s.Table("pods")
		if ts == nil || !ts.Synced || len(ts.Rows) != 1 || ts.Error != "" {
			return false
		}
		ci := slices.IndexFunc(ts.Columns, func(c model.Column) bool { return c.ID == model.ColumnCPU })
		return ci >= 0 && ts.Rows[0].Cells[ci].Text != ""
	})
	if !slices.Equal(ts.Watched, []string{nsLimited}) || ts.Rows[0].Ref.Namespace != nsLimited {
		t.Errorf("watched %q, row %v", ts.Watched, ts.Rows[0].Ref)
	}

	d := dispatcher{t: t, ctx: ctx, s: s}
	ref := model.ObjectRef{Kind: "deployments", Namespace: nsLimited, Name: "web"}
	start := time.Now()
	var m map[string]any
	d.must("object.get", map[string]any{"ref": ref}, &m)
	if m["missing"] == true || m["loading"] == true || time.Since(start) > 10*time.Second {
		t.Errorf("object.get after %s: missing %v, loading %v", time.Since(start), m["missing"], m["loading"])
	}

	start = time.Now()
	var tab commands.DockResult
	d.must("workload.logs", map[string]any{"ref": ref}, &tab)
	stream, err := s.OpenLogs(ctx, tab.Tab.ID)
	if err != nil {
		t.Fatalf("the Deployment's logs: %v", err)
	}
	collect(stream).waitLines(ctx, t, "the web pod's line", func(got []string) bool {
		return slices.ContainsFunc(got, func(l string) bool { return strings.Contains(l, "hello from web") })
	})
	_ = stream.Close()
	if d := time.Since(start); d > 15*time.Second {
		t.Errorf("the logs took %s", d)
	}

	var mr engine.MetricsResult
	d.must("metrics.get", map[string]any{"kind": "pods"}, &mr)
	if mr.Status != metrics.StatusAvailable || len(mr.Pods) != 1 || mr.Pods[0].Namespace != nsLimited {
		t.Errorf("metrics.get: %+v", mr)
	}

	// Each kind: one refused streaming watch in all namespaces (and the plain list client-go falls
	// back to), then one in cogpit-ns-a; the definitions, which the user may not list, once and no
	// more (customkinds.go drop).
	b := s.Backend()
	st := b.Conn.Counter().Snapshot()
	t.Logf("reads: %v", st.ByVerbResource)
	for _, res := range []string{"pods", "deployments.apps", "replicasets.apps", "events"} {
		if l, w := st.ByVerbResource["list "+res], st.ByVerbResource["watch "+res]; l < 2 || l > 3 || w < 1 {
			t.Errorf("%s: %d lists and %d watches, want 2 lists (all namespaces refused, then cogpit-ns-a; one retry at most) and cogpit-ns-a's watch", res, l, w)
		}
	}
	if n := st.ByVerbResource["watchlist customresourcedefinitions.apiextensions.k8s.io"] + st.ByVerbResource["list customresourcedefinitions.apiextensions.k8s.io"]; n > 2 {
		t.Errorf("%d reads of the definitions the user may not list, want at most 2", n)
	}
	waitFor(ctx, t, "no read in all namespaces left", func() bool {
		for _, sc := range b.Cache.Stats() {
			if sc.Scope.Namespace == "" && sc.Scope.GVR.Resource != "customresourcedefinitions" && sc.Scope.GVR.Resource != "namespaces" {
				t.Logf("still read in all namespaces: %s (%s)", sc.Scope.String(), sc.Error)
				return false
			}
		}
		return true
	})
}
