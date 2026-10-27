//go:build kind

package integration_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/model"
)

// configNamespace is the shared kind cluster's namespace of TestKindConfigViews; the test creates
// it, creates objects only inside it, and deletes it.
const configNamespace = "cogpit-c"

// TestKindConfigViews fills the Config views from watches on the shared kind cluster. In its own
// namespace it creates a Deployment with a PodDisruptionBudget, a ResourceQuota, a LimitRange and a
// HorizontalPodAutoscaler, and checks the rows the profiles make of what the controllers report
// (of the autoscaler, its spec alone); the cluster-scoped kinds (priority classes and their pod
// counts, runtime classes, webhook configurations, admission policies and bindings) and the leases
// are read, never written.
func TestKindConfigViews(t *testing.T) {
	kubeconfig, contextName := os.Getenv("COGPIT_KIND_KUBECONFIG"), os.Getenv("COGPIT_KIND_CONTEXT")
	if kubeconfig == "" || contextName == "" {
		t.Skip("COGPIT_KIND_KUBECONFIG and COGPIT_KIND_CONTEXT are not set; run make test-kind")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	conn, err := cluster.Connect(ctx, cluster.Options{KubeconfigPaths: []string{kubeconfig}, Context: contextName})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	kube, ns := conn.Kube(), configNamespace
	freshNamespace(ctx, t, kube, ns)

	// The quota and the limit range first, so that the deployment's pod is counted and defaulted.
	if _, err := kube.CoreV1().ResourceQuotas(ns).Create(ctx, &corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: "compute"},
		Spec: corev1.ResourceQuotaSpec{Hard: corev1.ResourceList{
			corev1.ResourcePods: resource.MustParse("10"), corev1.ResourceRequestsCPU: resource.MustParse("1"),
		}},
	}, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := kube.CoreV1().LimitRanges(ns).Create(ctx, &corev1.LimitRange{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: "defaults"},
		Spec: corev1.LimitRangeSpec{Limits: []corev1.LimitRangeItem{{
			Type: corev1.LimitTypeContainer,
			Max:  corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("500m")},
		}}},
	}, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := kube.AppsV1().Deployments(ns).Create(ctx, &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: "web"},
		Spec: appsv1.DeploymentSpec{Replicas: int32p(1), Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "web"}},
			Template: podTemplate("web", nil)},
	}, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	// minAvailable 1 over one pod: the budget is met but allows no disruption.
	if _, err := kube.PolicyV1().PodDisruptionBudgets(ns).Create(ctx, guardingBudget(ns, "web-pdb", "web"), metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	utilization := int32(80)
	if _, err := kube.AutoscalingV2().HorizontalPodAutoscalers(ns).Create(ctx, &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: "web"},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{APIVersion: "apps/v1", Kind: "Deployment", Name: "web"},
			MinReplicas:    int32p(1), MaxReplicas: 3,
			Metrics: []autoscalingv2.MetricSpec{{Type: autoscalingv2.ResourceMetricSourceType, Resource: &autoscalingv2.ResourceMetricSource{
				Name: corev1.ResourceCPU, Target: autoscalingv2.MetricTarget{Type: autoscalingv2.UtilizationMetricType, AverageUtilization: &utilization},
			}}},
		},
	}, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}

	s := engine.New(engine.Options{Connector: &engine.ClusterConnector{KubeconfigPaths: []string{kubeconfig}}})
	defer s.Close()
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	if err := s.Connect(ctx, contextName); err != nil {
		t.Fatal(err)
	}
	d := dispatcher{t: t, ctx: ctx, s: s}

	// Kubernetes 1.36 and later serve every admission policy kind at v1.
	for _, key := range []string{"mutatingadmissionpolicies", "mutatingadmissionpolicybindings", "validatingadmissionpolicies", "validatingadmissionpolicybindings"} {
		p, ok := s.Backend().Profile(key)
		if !ok || p.Version != "v1" {
			t.Errorf("%s: offered %v at %+v; want v1 on Kubernetes 1.36 and later", key, ok, p)
		}
	}

	// row waits for the view's row of ref to satisfy cond and returns it with the view's columns.
	row := func(view string, ref model.ObjectRef, what string, cond func(cell func(id string) model.Cell, r model.Row) bool) model.Row {
		t.Helper()
		d.must("ui.openView", map[string]any{"view": view}, nil)
		var got model.Row
		waitFor(ctx, t, view+": "+what, func() bool {
			tbl := s.Table(view)
			if tbl == nil || !tbl.Synced {
				return false
			}
			i := tbl.IndexOf(ref)
			if i < 0 {
				return false
			}
			got = tbl.Rows[i]
			cell := func(id string) model.Cell {
				for j, c := range tbl.Columns {
					if c.ID == id && j < len(got.Cells) {
						return got.Cells[j]
					}
				}
				return model.Cell{}
			}
			return cond(cell, got)
		})
		return got
	}
	in := func(kind, name string) model.ObjectRef { return model.ObjectRef{Kind: kind, Namespace: ns, Name: name} }

	row("poddisruptionbudgets", in("poddisruptionbudgets", "web-pdb"), "a met budget that allows no disruption",
		func(cell func(string) model.Cell, r model.Row) bool {
			return cell("minAvailable").Text == "1" && cell("maxUnavailable").Text == "N/A" &&
				cell("currentHealthy").Num == 1 && cell("desiredHealthy").Num == 1 &&
				cell("warning").Status == model.StatusWarning && strings.HasPrefix(cell("warning").Text, "Allows no disruptions")
		})
	row("resourcequotas", in("resourcequotas", "compute"), "the quota with its use counted",
		func(_ func(string) model.Cell, r model.Row) bool { return r.Status == model.StatusOK })
	row("limitranges", in("limitranges", "defaults"), "the limit range", func(func(string) model.Cell, model.Row) bool { return true })
	lr, err := s.DetailWait(ctx, in("limitranges", "defaults"))
	if err != nil {
		t.Fatal(err)
	}
	if !hasTableRow(lr, "Limits", []string{"Container", "cpu", "", "500m"}) {
		t.Errorf("limit range drawer: %+v", lr.Sections)
	}
	// The autoscaler's spec: its conditions come with the HPA controller's sync, 15 s after it was
	// created; how a row shows them is the goldens' (kinds/testdata/horizontalpodautoscalers).
	hpa := row("horizontalpodautoscalers", in("horizontalpodautoscalers", "web"), "the autoscaler's target and bounds",
		func(cell func(string) model.Cell, _ model.Row) bool {
			return strings.HasPrefix(cell("metrics").Text, "cpu: ") && strings.HasSuffix(cell("metrics").Text, "/80%") &&
				cell("minPods").Num == 1 && cell("maxPods").Num == 3
		})
	t.Logf("autoscaler: status %q", hpa.Status)

	// Pod counts of priority classes: the pods that name the class, from the cached pods.
	for _, class := range []string{"system-cluster-critical", "system-node-critical"} {
		row("priorityclasses", model.ObjectRef{Kind: "priorityclasses", Name: class}, class+"'s pod count", func(cell func(string) model.Cell, _ model.Row) bool {
			pods, err := kube.CoreV1().Pods("").List(ctx, metav1.ListOptions{})
			if err != nil {
				return false
			}
			want := 0
			for _, p := range pods.Items {
				if p.Spec.PriorityClassName == class {
					want++
				}
			}
			return want > 0 && cell("pods").Num == int64(want) && cell("globalDefault").Text == "false"
		})
	}

	// A node's heartbeat lease links its holder to the node.
	nodes, err := kube.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil || len(nodes.Items) == 0 {
		t.Fatalf("nodes: %v", err)
	}
	node := nodes.Items[0].Name
	row("leases", model.ObjectRef{Kind: "leases", Namespace: "kube-node-lease", Name: node}, "the node's lease",
		func(cell func(string) model.Cell, _ model.Row) bool {
			h := cell("holder")
			return h.Text == node && h.Link != nil && *h.Link == model.ObjectRef{Kind: "nodes", Name: node}
		})

	// The cluster-scoped kinds this test does not write: each view opens and syncs.
	for _, view := range []string{"runtimeclasses", "mutatingwebhookconfigurations", "validatingwebhookconfigurations",
		"mutatingadmissionpolicies", "mutatingadmissionpolicybindings", "validatingadmissionpolicies", "validatingadmissionpolicybindings"} {
		d.must("ui.openView", map[string]any{"view": view}, nil)
		waitFor(ctx, t, view+" synced", func() bool {
			tbl := s.Table(view)
			return tbl != nil && tbl.Synced && tbl.Error == ""
		})
		t.Logf("view %s: %d rows", view, s.Table(view).Total)
	}
}

// hasTableRow reports whether a drawer section's table has a row that starts with the cells want.
func hasTableRow(m *engine.DetailModel, title string, want []string) bool {
	for _, sec := range m.Sections {
		if sec.Title != title || sec.Table == nil {
			continue
		}
		for _, r := range sec.Table.Rows {
			if len(r) >= len(want) && strings.Join(r[:len(want)], "|") == strings.Join(want, "|") {
				return true
			}
		}
	}
	return false
}
