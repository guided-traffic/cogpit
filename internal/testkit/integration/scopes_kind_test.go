//go:build kind

package integration_test

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/guided-traffic/cogpit/internal/cache"
	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// The namespaces of the Secrets kind tests; each test deletes its namespace afterwards.
const (
	scopesNamespace  = "cogpit-scopes"
	secretsNamespace = "cogpit-a"
)

// TestKindSecretScopes checks the covering scopes of architecture §5 against a real API server:
// a label-selected informer alone holds only the Secrets it selects, in full; once the unselected
// scope runs, the selected one reads from it (its own informer stops), sees an object whose labels
// leave the selection as a delete, and when the unselected scope's warm period ends it lists on its
// own again. A metadata-only informer (PartialObjectMetadata, streamed or listed) holds names and
// labels but no value. It creates Secrets in its own namespace and deletes the namespace afterwards.
func TestKindSecretScopes(t *testing.T) {
	kubeconfig, contextName := kindEnv(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	conn, err := cluster.Connect(ctx, cluster.Options{KubeconfigPaths: []string{kubeconfig}, Context: contextName})
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer conn.Close()
	kube := conn.Kube()
	kindNamespace(ctx, t, kube, scopesNamespace)
	for _, s := range []*corev1.Secret{
		{ObjectMeta: metav1.ObjectMeta{Name: "plain", Namespace: scopesNamespace}, StringData: map[string]string{"password": "hunter2"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "sh.helm.release.v1.x.v1", Namespace: scopesNamespace, Labels: map[string]string{"owner": "helm", "name": "x"}},
			Type: "helm.sh/release.v1", StringData: map[string]string{"release": "payload"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "sh.helm.release.v1.x.v2", Namespace: scopesNamespace, Labels: map[string]string{"owner": "helm", "name": "x"}},
			Type: "helm.sh/release.v1", StringData: map[string]string{"release": "payload 2"}},
	} {
		if _, err := kube.CoreV1().Secrets(scopesNamespace).Create(ctx, s, metav1.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
	}

	c := cache.New(cache.DynamicSource(conn.Dynamic(), conn.Metadata()), cache.Options{WarmPeriod: 500 * time.Millisecond})
	defer c.Close()
	helmScope := cache.Scope{GVR: testkit.SecretsGVR, Namespace: scopesNamespace, LabelSelector: "owner=helm"}
	fullScope := cache.Scope{GVR: testkit.SecretsGVR, Namespace: scopesNamespace}
	coveredBy := func() string {
		for _, st := range c.Stats() {
			if st.Scope == helmScope {
				return st.CoveredBy
			}
		}
		return "-"
	}
	wait := func(what string, cond func() bool) {
		t.Helper()
		if !testkit.WaitFor(ctx, cond) {
			t.Fatalf("%s: scopes %+v", what, c.Stats())
		}
	}

	helm := c.Acquire(helmScope)
	defer helm.Release()
	if !helm.WaitForSync(ctx) {
		t.Fatalf("owner=helm not synced: %v", helm.Err())
	}
	if l := helm.List(); len(l) != 2 || l[0].Object["data"] == nil {
		t.Fatalf("owner=helm scope alone: %d objects", len(l))
	}
	if coveredBy() != "" {
		t.Fatalf("covered without a cover: %q", coveredBy())
	}
	before := conn.Counter().Snapshot()

	full := c.Acquire(fullScope)
	if !full.WaitForSync(ctx) {
		t.Fatalf("full scope not synced: %v", full.Err())
	}
	wait("the move onto the full scope", func() bool { return coveredBy() == "secrets in "+scopesNamespace })
	if len(full.List()) != 3 || len(helm.List()) != 2 {
		t.Errorf("full %d, owner=helm %d", len(full.List()), len(helm.List()))
	}
	// An object whose labels leave the selection is deleted from the covered scope.
	deleted := make(chan string, 4)
	defer helm.Subscribe(func(ev cache.Event) {
		if ev.Type == cache.Deleted {
			deleted <- ev.Object.GetName()
		}
	})()
	if _, err := kube.CoreV1().Secrets(scopesNamespace).Patch(ctx, "sh.helm.release.v1.x.v1", types.MergePatchType,
		[]byte(`{"metadata":{"labels":{"owner":"someone-else"}}}`), metav1.PatchOptions{}); err != nil {
		t.Fatal(err)
	}
	select {
	case name := <-deleted:
		if name != "sh.helm.release.v1.x.v1" {
			t.Errorf("deleted %s", name)
		}
	case <-ctx.Done():
		t.Fatal("no delete for the Secret whose labels left the selection")
	}
	if len(helm.List()) != 1 || len(full.List()) != 3 {
		t.Errorf("after the label change: owner=helm %d, full %d", len(helm.List()), len(full.List()))
	}

	// The full scope's warm period ends: the owner=helm scope lists on its own again.
	full.Release()
	wait("the handover back", func() bool { return coveredBy() == "" && helm.HasSynced() && !c.Running(fullScope) })
	if l := helm.List(); len(l) != 1 || l[0].GetName() != "sh.helm.release.v1.x.v2" {
		t.Errorf("owner=helm after the handover: %d objects", len(l))
	}
	after := conn.Counter().Snapshot()
	initial := func(st cluster.RequestStats) int64 {
		return st.ByVerbResource["watchlist secrets"] + st.ByVerbResource["list secrets"]
	}
	if got := initial(after) - initial(before); got != 2 {
		t.Errorf("%d initial Secrets requests for the full scope and the handover back, want 2", got)
	}

	meta := c.Acquire(cache.Scope{GVR: testkit.SecretsGVR, Namespace: scopesNamespace, MetadataOnly: true})
	defer meta.Release()
	if !meta.WaitForSync(ctx) {
		t.Fatalf("metadata scope not synced: %v", meta.Err())
	}
	wait("the metadata scope", func() bool { return len(meta.List()) >= 3 })
	for _, o := range meta.List() {
		if _, has := o.Object["data"]; has || !cache.IsMetadataObject(o) || o.GetUID() == "" {
			t.Errorf("metadata scope holds %v", o.Object)
		}
	}
	t.Logf("requests: %v", conn.Counter().Snapshot().ByVerbResource)
}
