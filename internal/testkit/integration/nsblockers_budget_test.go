package integration_test

import (
	"context"
	"maps"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// TestBudgetTerminatingNamespaceBlockers measures what a Terminating namespace's Blocking Deletion
// list costs at the HTTP level (WF43, D-98), for a user whose rules allow Config Maps and not
// Secrets: one metadata-only initial list (a streaming watch) of the Config Maps in that namespace,
// nothing cluster-wide and nothing whole, no request for Secrets (the rules review said no, the row
// counts them from the condition), and nothing more while the drawer stays open.
func TestBudgetTerminatingNamespaceBlockers(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	all := testkit.FakeRule{Verbs: []string{"get", "list", "watch"}, APIGroups: []string{"*"}, Resources: []string{"namespaces", "configmaps", "pods", "services", "deployments", "events"}}
	srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{
		Token:  "budget-token",
		Access: &testkit.FakeAccess{Enforce: true, Rules: map[string][]testkit.FakeRule{"": {all}}},
		Resources: append(append([]testkit.FakeResource(nil), testkit.DefaultFakeResources...),
			testkit.FakeResource{GVR: configMapsGVR, Kind: "ConfigMap", Namespaced: true},
			testkit.FakeResource{GVR: testkit.SecretsGVR, Kind: "Secret", Namespaced: true}),
	})
	defer srv.Close()
	srv.Set(testkit.NamespacesGVR, testkit.Object("v1", "Namespace", "", "walk-fin", map[string]any{
		"metadata": map[string]any{"deletionTimestamp": "2026-10-19T10:00:00Z"},
		"status": map[string]any{"phase": "Terminating", "conditions": []any{map[string]any{
			"type": "NamespaceContentRemaining", "status": "True",
			"message": "Some resources are remaining: configmaps. has 1 resource instances, secrets. has 1 resource instances",
		}}},
	}))
	held := func(kind, ns, name string) *unstructured.Unstructured {
		return testkit.Object("v1", kind, ns, name, map[string]any{
			"metadata": map[string]any{"deletionTimestamp": "2026-10-19T10:00:00Z", "finalizers": []any{"example.com/x"}},
			"data":     map[string]any{"k": "v"},
		})
	}
	srv.Set(configMapsGVR, held("ConfigMap", "walk-fin", "walk-cm"))
	srv.Set(configMapsGVR, testkit.Object("v1", "ConfigMap", "elsewhere", "other", nil))
	srv.Set(testkit.SecretsGVR, held("Secret", "walk-fin", "walk-secret"))

	s := engine.New(engine.Options{
		Connector:        &engine.ClusterConnector{KubeconfigPaths: []string{srv.WriteKubeconfig(t, "fake", "")}},
		ClusterSettings:  engine.NewClusterSettingsStore(t.TempDir()),
		SnapshotInterval: 10 * time.Millisecond,
	})
	t.Cleanup(s.Close)
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	if err := s.Connect(ctx, "fake"); err != nil {
		t.Fatal(err)
	}
	ref := model.ObjectRef{Context: "fake", Kind: "namespaces", Name: "walk-fin"}
	s.UpdateUI(func(u *engine.UIState) { u.Drawer = &ref })
	var text string
	if !testkit.WaitFor(ctx, func() bool {
		m, err := s.Detail(ref)
		if err != nil {
			return false
		}
		var b strings.Builder
		for _, sec := range m.Sections {
			if sec.Title == engine.BlockingTitle {
				for _, it := range sec.Items {
					b.WriteString(it.Name + ": " + it.Value + "\n")
				}
			}
		}
		text = b.String()
		return strings.Contains(text, "Config Map: walk-cm") && strings.Contains(text, "Secrets: 1 remaining; you may not list secrets in walk-fin")
	}) {
		t.Fatalf("the list never showed: %q, requests %v", text, srv.RequestsIn())
	}
	reads := func() map[string]int64 {
		out := map[string]int64{}
		for k, v := range srv.RequestsIn() {
			if strings.Contains(k, " configmaps") || strings.Contains(k, " secrets") {
				out[k] = v
			}
		}
		return out
	}
	if !testkit.WaitFor(ctx, func() bool { return srv.OpenWatchesBy()["configmaps (metadata)"] == 1 }) {
		t.Fatalf("open watches %v", srv.OpenWatchesBy())
	}
	want := map[string]int64{"list configmaps (metadata) in walk-fin": 1, "watch configmaps (metadata) in walk-fin": 1}
	if got := reads(); !maps.Equal(got, want) {
		t.Errorf("reads %v, want %v (metadata only, in walk-fin, no Secrets)", got, want)
	}
	time.Sleep(300 * time.Millisecond) // the drawer stays open: no poll, no relist
	if got := reads(); !maps.Equal(got, want) {
		t.Errorf("reads while open %v, want %v", got, want)
	}
}
