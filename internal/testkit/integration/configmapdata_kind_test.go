//go:build kind

package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// configMapDataNamespace is the namespace the drawer's Config Map editing test owns on the shared
// kind cluster.
const configMapDataNamespace = "cogpit-cmdata"

// TestKindConfigMapDataSave walks the drawer's Save of a Config Map's values (the owner's F3,
// D-55) against the kind cluster, in its own namespace: configMap.readData reads the values whole
// from the cache, configMap.reviewData diffs a change and the API server accepts its dry run,
// configMap.saveData writes it (the other keys and a final newline kept); another client changes
// the Config Map and a save based on the old version writes nothing and reports the conflict with
// the newer version, which a second save naming it writes over, the other client's change kept;
// an immutable Config Map is refused before anything is sent. The namespace is deleted at the end.
func TestKindConfigMapDataSave(t *testing.T) {
	kubeconfig, contextName := os.Getenv("COGPIT_KIND_KUBECONFIG"), os.Getenv("COGPIT_KIND_CONTEXT")
	if kubeconfig == "" || contextName == "" {
		t.Skip("COGPIT_KIND_KUBECONFIG and COGPIT_KIND_CONTEXT are not set; run make test-kind")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	s := engine.New(engine.Options{Connector: &engine.ClusterConnector{KubeconfigPaths: []string{kubeconfig}}})
	defer s.Close()
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	if err := s.Connect(ctx, contextName); err != nil {
		t.Fatal(err)
	}
	conn := s.Backend().Conn
	dyn := conn.Dynamic()
	run := func(id string, params any, out any) error {
		t.Helper()
		raw, err := json.Marshal(params)
		if err != nil {
			t.Fatal(err)
		}
		res, err := s.Dispatch(ctx, engine.Invocation{ID: id, Params: raw, Origin: engine.OriginGUI})
		if err == nil && out != nil {
			b, _ := json.Marshal(res)
			if err := json.Unmarshal(b, out); err != nil {
				t.Fatalf("%s: %v", id, err)
			}
		}
		return err
	}
	must := func(id string, params any, out any) {
		t.Helper()
		if err := run(id, params, out); err != nil {
			t.Fatalf("%s: %v", id, err)
		}
	}

	if !testkit.WaitFor(ctx, func() bool {
		_, err := dyn.Resource(kindNamespaces).Get(ctx, configMapDataNamespace, metav1.GetOptions{})
		return apierrors.IsNotFound(err)
	}) {
		t.Fatalf("namespace %s still exists from an earlier run", configMapDataNamespace)
	}
	t.Cleanup(func() {
		cctx, ccancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer ccancel()
		if err := conn.DeleteObject(cctx, kindNamespaces, "", configMapDataNamespace, ""); err != nil && !apierrors.IsNotFound(err) {
			t.Errorf("cleanup: %v", err)
		}
	})
	var created engine.CreateResult
	must("object.create", map[string]any{"yaml": `apiVersion: v1
kind: Namespace
metadata:
  name: ` + configMapDataNamespace + `
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: cilium-config
  namespace: ` + configMapDataNamespace + `
data:
  agent-not-ready-taint-key: node.cilium.io/agent-not-ready
  bpf-lb-acceleration: disabled
  hubble-flowlogs-config.yaml: |
    flowLogs:
      - name: all
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: frozen
  namespace: ` + configMapDataNamespace + `
immutable: true
data:
  a: "1"
`}, &created)
	if created.Created != 3 {
		t.Fatalf("create: %+v", created)
	}
	ref := model.ObjectRef{Kind: "configmaps", Namespace: configMapDataNamespace, Name: "cilium-config"}

	var data engine.ConfigMapData
	if !testkit.WaitFor(ctx, func() bool { return run("configMap.readData", map[string]any{"ref": ref}, &data) == nil }) {
		t.Fatal("configMap.readData never found the Config Map")
	}
	values := map[string]string{}
	for _, v := range data.Values {
		values[v.Key] = v.Value
	}
	if len(values) != 3 || values["hubble-flowlogs-config.yaml"] != "flowLogs:\n  - name: all\n" || data.ReadOnly != "" {
		t.Fatalf("read %+v", data)
	}

	change := map[string]any{"ref": ref, "resourceVersion": data.ResourceVersion, "data": map[string]string{"bpf-lb-acceleration": "native"}}
	var review engine.ConfigMapDataReview
	must("configMap.reviewData", change, &review)
	if !review.Accepted || review.Stale || !review.Changed || len(review.Keys) != 1 {
		t.Fatalf("review %+v", review)
	}
	var saved engine.ConfigMapDataSaved
	must("configMap.saveData", change, &saved)
	get := func() *unstructured.Unstructured {
		t.Helper()
		cur, err := dyn.Resource(kindConfigMaps).Namespace(configMapDataNamespace).Get(ctx, "cilium-config", metav1.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}
		return cur
	}
	cur := get()
	got, _, _ := unstructured.NestedStringMap(cur.Object, "data")
	if cur.GetResourceVersion() != saved.ResourceVersion || got["bpf-lb-acceleration"] != "native" ||
		got["hubble-flowlogs-config.yaml"] != values["hubble-flowlogs-config.yaml"] || got["agent-not-ready-taint-key"] != values["agent-not-ready-taint-key"] {
		t.Fatalf("after the save (version %s, saved %s): %v", cur.GetResourceVersion(), saved.ResourceVersion, got)
	}

	// Another client changes another key; a save based on the saved version writes nothing.
	cur.Object["data"].(map[string]any)["agent-not-ready-taint-key"] = "node.cilium.io/other-client"
	other, err := dyn.Resource(kindConfigMaps).Namespace(configMapDataNamespace).Update(ctx, cur, metav1.UpdateOptions{FieldManager: "other-client"})
	if err != nil {
		t.Fatal(err)
	}
	stale := map[string]any{"ref": ref, "resourceVersion": saved.ResourceVersion, "data": map[string]string{"bpf-lb-acceleration": "best-effort"}}
	err = run("configMap.saveData", stale, nil)
	var ce *engine.DataConflictError
	if !errors.As(err, &ce) {
		t.Fatalf("a save over another client's change: %v", err)
	}
	if v, _, _ := unstructured.NestedString(get().Object, "data", "bpf-lb-acceleration"); v != "native" {
		t.Fatalf("a refused save wrote %q", v)
	}
	if ce.CurrentResourceVersion == "" { // the cache had not seen it yet: once it has, the review names it
		if !testkit.WaitFor(ctx, func() bool {
			return run("configMap.reviewData", stale, &review) == nil && review.Stale && review.CachedResourceVersion == other.GetResourceVersion()
		}) {
			t.Fatal("the review never showed the newer version")
		}
		ce.CurrentResourceVersion = review.CachedResourceVersion
	}
	if ce.CurrentResourceVersion != other.GetResourceVersion() {
		t.Fatalf("the conflict names version %s, the other client wrote %s", ce.CurrentResourceVersion, other.GetResourceVersion())
	}
	stale["resourceVersion"] = ce.CurrentResourceVersion
	must("configMap.saveData", stale, &saved)
	got, _, _ = unstructured.NestedStringMap(get().Object, "data")
	if got["bpf-lb-acceleration"] != "best-effort" || got["agent-not-ready-taint-key"] != "node.cilium.io/other-client" {
		t.Fatalf("the save over the newer version: %v", got)
	}

	// Another client changes another key again; a review that names the edited key's value at the
	// saved version rebases onto the newer one (dry run included), and the save with it writes.
	cur = get()
	cur.Object["data"].(map[string]any)["agent-not-ready-taint-key"] = "node.cilium.io/third-client"
	if other, err = dyn.Resource(kindConfigMaps).Namespace(configMapDataNamespace).Update(ctx, cur, metav1.UpdateOptions{FieldManager: "other-client"}); err != nil {
		t.Fatal(err)
	}
	rebase := map[string]any{"ref": ref, "resourceVersion": saved.ResourceVersion, "data": map[string]string{"bpf-lb-acceleration": "native"},
		"from": map[string]string{"bpf-lb-acceleration": "best-effort"}}
	review = engine.ConfigMapDataReview{}
	must("configMap.reviewData", rebase, &review)
	if review.RebasedFrom != saved.ResourceVersion || review.BaseResourceVersion != other.GetResourceVersion() || review.Stale || !review.Accepted {
		t.Fatalf("a review over another key's change (version %s): %+v", other.GetResourceVersion(), review)
	}
	must("configMap.saveData", map[string]any{"ref": ref, "resourceVersion": review.BaseResourceVersion, "data": rebase["data"]}, &saved)
	got, _, _ = unstructured.NestedStringMap(get().Object, "data")
	if got["bpf-lb-acceleration"] != "native" || got["agent-not-ready-taint-key"] != "node.cilium.io/third-client" {
		t.Fatalf("the rebased save: %v", got)
	}

	// Keys added and removed (the owner's audit decision of 2026-10-16): the server accepts the dry
	// run of an edit that adds a key of 253 characters and one more and removes one; the save is
	// destructive and asks the confirmer, naming the keys; the Config Map then holds the new keys and
	// not the removed one.
	var asked []engine.ConfirmRequest
	s.SetConfirmer(engine.ConfirmFunc(func(_ context.Context, req engine.ConfirmRequest) (bool, error) {
		asked = append(asked, req)
		return true, nil
	}))
	longKey := strings.Repeat("k", 253)
	keys := map[string]any{"ref": ref, "resourceVersion": saved.ResourceVersion, "add": map[string]string{longKey: "v", "log-level": "debug"},
		"remove": []string{"hubble-flowlogs-config.yaml"}}
	if !testkit.WaitFor(ctx, func() bool {
		review = engine.ConfigMapDataReview{}
		return run("configMap.reviewData", keys, &review) == nil && !review.Stale
	}) {
		t.Fatal("the cache never saw the rebased save")
	}
	if !review.Accepted || len(review.KeysAdded) != 2 || len(review.KeysRemoved) != 1 || review.KeysRemoved[0] != "hubble-flowlogs-config.yaml" {
		t.Fatalf("the review of keys: %+v", review)
	}
	must("configMap.saveData", keys, &saved)
	if len(asked) != 1 || asked[0].Danger != engine.DangerDestructive || !strings.HasPrefix(asked[0].Detail, "Removes: hubble-flowlogs-config.yaml. Adds: ") {
		t.Errorf("the confirmation of a removal: %+v", asked)
	}
	got, _, _ = unstructured.NestedStringMap(get().Object, "data")
	if _, kept := got["hubble-flowlogs-config.yaml"]; kept || got[longKey] != "v" || got["log-level"] != "debug" || got["bpf-lb-acceleration"] != "native" {
		t.Fatalf("after the save of keys: %v", got)
	}

	// An immutable Config Map: refused before anything is sent.
	frozen := model.ObjectRef{Kind: "configmaps", Namespace: configMapDataNamespace, Name: "frozen"}
	var fd engine.ConfigMapData
	if !testkit.WaitFor(ctx, func() bool { return run("configMap.readData", map[string]any{"ref": frozen}, &fd) == nil }) {
		t.Fatal("configMap.readData never found the immutable Config Map")
	}
	err = run("configMap.saveData", map[string]any{"ref": frozen, "resourceVersion": fd.ResourceVersion, "data": map[string]string{"a": "2"}}, nil)
	if err == nil || !strings.Contains(err.Error(), "immutable") {
		t.Errorf("a save of an immutable Config Map: %v", err)
	}
}
