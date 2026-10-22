//go:build kind

package integration_test

import (
	"context"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// trivyReportsKey is the kind key of Trivy Operator's vulnerability reports.
const trivyReportsKey = "vulnerabilityreports.aquasecurity.github.io"

// TestKindTrivy checks the Trivy integration (D-78) against a real Trivy Operator: `make
// kind-trivy` installs it into the kind cluster, scanning only hack/kind/trivy's workloads in
// cogpit-trivy-a and cogpit-trivy-b. The integration says it found the reports; each pod's drawer
// rates its images from its controller's reports (a ReplicaSet's of two images and an init
// container, a StatefulSet's, a Job's, the CronJob's of the Job made from it, a bare pod's own);
// the Pods list rates every pod by its worst image; switched off, nothing is held and the column is
// gone. It logs what the reports cost in the cache, trimmed as the integration reads them and whole
// as their own view does. Skipped without the operator.
func TestKindTrivy(t *testing.T) {
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
	if _, err := conn.Dynamic().Resource(testkit.CRDsGVR).Get(ctx, trivyReportsKey, metav1.GetOptions{}); apierrors.IsNotFound(err) {
		t.Skip("Trivy Operator is not installed; run make kind-trivy")
	} else if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Kube().CoreV1().Namespaces().Get(ctx, "cogpit-trivy-a", metav1.GetOptions{}); apierrors.IsNotFound(err) {
		t.Skip("the Trivy test workloads are not there; run make kind-trivy")
	}

	s := engine.New(engine.Options{Connector: &engine.ClusterConnector{KubeconfigPaths: []string{kubeconfig}}})
	defer s.Close()
	if err := s.Connect(ctx, contextName); err != nil {
		t.Fatal(err)
	}
	trivy := func() engine.IntegrationStatus {
		for _, st := range s.IntegrationStatuses(contextName) {
			if st.Name == engine.IntegrationTrivy {
				return st
			}
		}
		return engine.IntegrationStatus{}
	}
	waitFor(ctx, t, "the Trivy integration found", func() bool { return trivy().State == engine.IntegrationFound })
	if st := trivy(); st.Where != "aquasecurity.github.io/v1alpha1" {
		t.Errorf("status %+v", st)
	}

	// pod returns the name of the first pod with a label in a namespace.
	pod := func(ns, selector string) string {
		list, err := conn.Kube().CoreV1().Pods(ns).List(ctx, metav1.ListOptions{LabelSelector: selector})
		if err != nil || len(list.Items) == 0 {
			t.Fatalf("no pod %s in %s: %v", selector, ns, err)
		}
		return list.Items[0].Name
	}
	// grades returns a pod's container sections' image grades as "container: D · 1 Critical", once
	// the drawer has them (its reports may still be on their way).
	grades := func(ref model.ObjectRef) map[string]string {
		m, err := s.DetailWait(ctx, ref)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]string{}
		for _, sec := range m.Sections {
			if sec.Grade != nil {
				out[sec.Title] = sec.Grade.Text
			}
		}
		return out
	}
	worst := map[string]string{} // pod name → the grade of its Image Rating in the Pods list
	// The grades as Trivy's database rated the images on 2026-10-22 (D-113): nginx:1.16.1's dozens of
	// Critical E; nginx:1.30.0-alpine's High C; alpine:3.12's Critical D (its OS's end of life caps at
	// C, and D is worse); busybox's nothing S.
	for _, c := range []struct {
		ns, selector string
		containers   map[string]string // container → its grade's letter
		worst        string
	}{
		{"cogpit-trivy-a", "app=mixed", map[string]string{"init": "S", "old": "E", "new": "S"}, "E"},
		{"cogpit-trivy-a", "app=store", map[string]string{"web": "C"}, "C"},
		{"cogpit-trivy-a", "batch.kubernetes.io/job-name=once", map[string]string{"task": "S"}, "S"},
		{"cogpit-trivy-a", "batch.kubernetes.io/job-name=nightly-29000000", map[string]string{"report": "D"}, "D"},
		{"cogpit-trivy-b", "", map[string]string{"shell": "D"}, "D"},
	} {
		ref := model.ObjectRef{Kind: "pods", Namespace: c.ns, Name: pod(c.ns, c.selector)}
		worst[ref.Name] = c.worst
		waitFor(ctx, t, "the grades of "+ref.String(), func() bool {
			got := grades(ref)
			if len(got) != len(c.containers) {
				return false
			}
			for name, want := range c.containers {
				if g, ok := got[name]; !ok || !strings.HasPrefix(g, want+" · ") {
					return false
				}
			}
			return true
		})
		t.Logf("%s: %v", ref, grades(ref))
	}

	// The Pods list's Image Rating: each test pod's worst grade.
	s.UpdateUI(func(u *engine.UIState) { u.View = "pods" })
	cellOf := func(snap *engine.TableSnapshot, name string) model.Cell {
		ci := slices.IndexFunc(snap.Columns, func(c model.Column) bool { return c.ID == "imageRating" })
		for _, r := range snap.Rows {
			if r.Ref.Name == name && ci >= 0 && ci < len(r.Cells) {
				return r.Cells[ci]
			}
		}
		return model.Cell{}
	}
	waitFor(ctx, t, "the Pods list's grades", func() bool {
		snap := s.Table("pods")
		if snap == nil || !snap.Synced {
			return false
		}
		for name, want := range worst {
			if c := cellOf(snap, name); c.Grade.String() != want || !strings.HasPrefix(c.Text, want+" · ") {
				return false
			}
		}
		return true
	})
	for name := range worst {
		t.Logf("Pods list: %s: %q", name, cellOf(s.Table("pods"), name).Text)
	}

	// What the reports cost in the cache: the integration's trimmed scope, and the whole reports as
	// their own view reads them.
	p, _ := s.Backend().Profile(trivyReportsKey)
	whole := s.Backend().Acquire(p)
	waitFor(ctx, t, "the whole reports listed", whole.HasSynced)
	var trimmed, full int64
	for _, is := range s.Backend().Cache.StatsWithMemory() {
		if is.Scope.GVR.Resource != "vulnerabilityreports" {
			continue
		}
		t.Logf("cache: %s: %d reports, %d bytes (%d sized)", is.Scope, is.Objects, is.Bytes, is.Sized)
		if is.Scope.Trim != "" {
			trimmed += is.Bytes
		} else {
			full += is.Bytes
		}
	}
	whole.Release()
	if trimmed == 0 || full == 0 || trimmed*5 > full {
		t.Errorf("the trimmed reports take %d bytes, the whole ones %d", trimmed, full)
	}

	// Switched off: the handle goes back, the drawer shows no grade, the column is gone, the state
	// says off.
	s.UpdateUI(func(u *engine.UIState) {
		if u.ClusterSettings == nil {
			u.ClusterSettings = map[string]engine.ClusterSettings{}
		}
		cs := u.ClusterSettings[contextName]
		cs.SetIntegrations(0, 0, engine.IntTrivy)
		u.ClusterSettings[contextName] = cs
	})
	waitFor(ctx, t, "the reports released", func() bool {
		for _, is := range s.Backend().Cache.Stats() {
			if is.Scope.GVR.Resource == "vulnerabilityreports" && is.Handles > 0 {
				return false
			}
		}
		return true
	})
	if st := trivy(); st.State != engine.IntegrationOff {
		t.Errorf("off: %+v", st)
	}
	bare := model.ObjectRef{Kind: "pods", Namespace: "cogpit-trivy-b", Name: "bare"}
	if got := grades(bare); len(got) != 0 {
		t.Errorf("off, the drawer grades %v", got)
	}
	waitFor(ctx, t, "the column gone", func() bool {
		snap := s.Table("pods")
		return snap != nil && !slices.ContainsFunc(snap.Columns, func(c model.Column) bool { return c.ID == "imageRating" })
	})
}
