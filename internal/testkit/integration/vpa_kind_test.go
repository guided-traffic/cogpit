//go:build kind

package integration_test

import (
	"context"
	"os"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// vpaNamespace is TestKindVerticalPodAutoscalers' namespace; vpaKey the kind's key and its
// definition's name.
const (
	vpaNamespace = "cogpit-vpa"
	vpaKey       = "verticalpodautoscalers.autoscaling.k8s.io"
)

// TestKindVerticalPodAutoscalers installs a VerticalPodAutoscaler definition on the kind cluster
// (the definition only, no autoscaler components, with a schema that keeps any field), creates an
// autoscaler with the status a recommender would write, and checks the view: Cogpit's curated
// profile with Lens's documented columns, the row as the profile renders it, and the kind gone
// with its definition. A cluster that already has the definition is left alone (the test skips).
func TestKindVerticalPodAutoscalers(t *testing.T) {
	kubeconfig, contextName := os.Getenv("COGPIT_KIND_KUBECONFIG"), os.Getenv("COGPIT_KIND_CONTEXT")
	if kubeconfig == "" || contextName == "" {
		t.Skip("COGPIT_KIND_KUBECONFIG and COGPIT_KIND_CONTEXT are not set; run make test-kind")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	conn, err := cluster.Connect(ctx, cluster.Options{KubeconfigPaths: []string{kubeconfig}, Context: contextName})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	crds := conn.Dynamic().Resource(testkit.CRDsGVR)
	if _, err := crds.Get(ctx, vpaKey, metav1.GetOptions{}); err == nil {
		t.Skip("the cluster has a VerticalPodAutoscaler definition of its own; the test installs its own only")
	} else if !apierrors.IsNotFound(err) {
		t.Fatal(err)
	}
	deleteCRD := func() {
		dctx, dcancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer dcancel()
		if err := crds.Delete(dctx, vpaKey, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			t.Errorf("deleting the VPA definition: %v", err)
		}
	}
	t.Cleanup(deleteCRD)
	freshNamespace(ctx, t, conn.Kube(), vpaNamespace)

	crd := testkit.CRD("autoscaling.k8s.io", "verticalpodautoscalers", "VerticalPodAutoscaler", true)
	crd.SetUID("")
	crd.SetCreationTimestamp(metav1.Time{})
	crd.Object["status"] = nil
	// *.k8s.io groups are protected: the API server wants the approval annotation the autoscaler's
	// own definition carries.
	crd.SetAnnotations(map[string]string{"api-approved.kubernetes.io": "https://github.com/kubernetes/kubernetes/pull/63797"})
	if _, err := crds.Create(ctx, crd, metav1.CreateOptions{}); err != nil {
		t.Fatalf("creating the VPA definition: %v", err)
	}

	s := engine.New(engine.Options{Connector: &engine.ClusterConnector{KubeconfigPaths: []string{kubeconfig}}})
	defer s.Close()
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	if err := s.Connect(ctx, contextName); err != nil {
		t.Fatal(err)
	}
	waitFor(ctx, t, "the Vertical Pod Autoscalers kind", func() bool { _, ok := s.Backend().Profile(vpaKey); return ok })
	if p, _ := s.Backend().Profile(vpaKey); p.Title != "Vertical Pod Autoscalers" || p.ColumnIndex("mode") != 2 {
		t.Fatalf("profile %q with columns %+v; want the curated one", p.Title, p.Columns)
	}

	vpa := testkit.Object("autoscaling.k8s.io/v1", "VerticalPodAutoscaler", vpaNamespace, "web", map[string]any{
		"spec": map[string]any{
			"targetRef":    map[string]any{"apiVersion": "apps/v1", "kind": "Deployment", "name": "web"},
			"updatePolicy": map[string]any{"updateMode": "Recreate"},
		},
		"status": map[string]any{
			"conditions": []any{map[string]any{"type": "RecommendationProvided", "status": "True", "lastTransitionTime": "2026-10-13T00:00:00Z"}},
			"recommendation": map[string]any{"containerRecommendations": []any{
				map[string]any{"containerName": "web", "target": map[string]any{"cpu": "250m", "memory": "256Mi"}},
			}},
		},
	})
	vpa.SetUID("")
	vpa.SetCreationTimestamp(metav1.Time{})
	gvr := schema.GroupVersionResource{Group: "autoscaling.k8s.io", Version: "v1", Resource: "verticalpodautoscalers"}
	if _, err := conn.Dynamic().Resource(gvr).Namespace(vpaNamespace).Create(ctx, vpa, metav1.CreateOptions{}); err != nil {
		t.Fatalf("creating the autoscaler: %v", err)
	}

	d := dispatcher{t: t, ctx: ctx, s: s}
	d.must("ui.openView", map[string]any{"view": vpaKey}, nil)
	ref := model.ObjectRef{Kind: vpaKey, Namespace: vpaNamespace, Name: "web"}
	waitFor(ctx, t, "the autoscaler's row", func() bool {
		tbl := s.Table(vpaKey)
		if tbl == nil || !tbl.Synced {
			return false
		}
		i := tbl.IndexOf(ref)
		if i < 0 {
			return false
		}
		cell := func(id string) model.Cell {
			for j, c := range tbl.Columns {
				if c.ID == id {
					return tbl.Rows[i].Cells[j]
				}
			}
			return model.Cell{}
		}
		return cell("mode").Text == "Recreate" && cell("status").Text == "RecommendationProvided" &&
			cell("status").Status == model.StatusOK && cell("target").Text == "Deployment/web" && cell("cpuTarget").Text == "250m"
	})

	deleteCRD()
	waitFor(ctx, t, "the kind to go with its definition", func() bool { _, ok := s.Backend().Profile(vpaKey); return !ok })
}
