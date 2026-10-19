//go:build kind

package integration_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// secretDataNamespace is the namespace the drawer's Secret editing test owns on the shared kind
// cluster.
const secretDataNamespace = "cogpit-secdata"

var kindSecretsGVR = schema.GroupVersionResource{Version: "v1", Resource: "secrets"}

// TestKindSecretDataSave walks the drawer's Save of a Secret's values (the owner's audit answer of
// 2026-10-15, D-59) against the kind cluster, in its own namespace: the window reveals a value
// whole, secret.reviewData shows a masked diff and the API server accepts its dry run,
// secret.saveData writes it base64-encoded (the other keys kept); another client changes another
// key and the review rebases onto it; a change of the edited key is the conflict, written over by
// a second save naming it; an agent is refused before anything is sent. Measured too: kubectl's
// last-applied annotation, which Cogpit's cache drops from Secrets, does not survive a save
// (H-103). The namespace is deleted at the end.
func TestKindSecretDataSave(t *testing.T) {
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
	runAs := func(origin engine.Origin, id string, params any, out any) error {
		t.Helper()
		raw, err := json.Marshal(params)
		if err != nil {
			t.Fatal(err)
		}
		res, err := s.Dispatch(ctx, engine.Invocation{ID: id, Params: raw, Origin: origin})
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
		if err := runAs(engine.OriginGUI, id, params, out); err != nil {
			t.Fatalf("%s: %v", id, err)
		}
	}
	get := func() *unstructured.Unstructured {
		t.Helper()
		cur, err := dyn.Resource(kindSecretsGVR).Namespace(secretDataNamespace).Get(ctx, "db", metav1.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}
		return cur
	}
	value := func(obj *unstructured.Unstructured, key string) string {
		enc, _, _ := unstructured.NestedString(obj.Object, "data", key)
		raw, _ := base64.StdEncoding.DecodeString(enc)
		return string(raw)
	}

	if !testkit.WaitFor(ctx, func() bool {
		_, err := dyn.Resource(kindNamespaces).Get(ctx, secretDataNamespace, metav1.GetOptions{})
		return apierrors.IsNotFound(err)
	}) {
		t.Fatalf("namespace %s still exists from an earlier run", secretDataNamespace)
	}
	t.Cleanup(func() {
		cctx, ccancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer ccancel()
		if err := conn.DeleteObject(cctx, kindNamespaces, "", secretDataNamespace, ""); err != nil && !apierrors.IsNotFound(err) {
			t.Errorf("cleanup: %v", err)
		}
	})
	var created engine.CreateResult
	must("object.create", map[string]any{"yaml": `apiVersion: v1
kind: Namespace
metadata:
  name: ` + secretDataNamespace + `
---
apiVersion: v1
kind: Secret
metadata:
  name: db
  namespace: ` + secretDataNamespace + `
  annotations:
    kubectl.kubernetes.io/last-applied-configuration: '{"kind":"Secret"}'
type: Opaque
stringData:
  password: kind-pass-1
  user: app
`}, &created)
	if created.Created != 2 {
		t.Fatalf("create: %+v", created)
	}
	ref := model.ObjectRef{Kind: "secrets", Namespace: secretDataNamespace, Name: "db"}
	must("ui.navigate", map[string]any{"ref": ref}, nil)
	var obj *unstructured.Unstructured
	if !testkit.WaitFor(ctx, func() bool {
		var err error
		obj, err = s.Lookup(ref)
		return err == nil
	}) {
		t.Fatal("the cache never held the Secret")
	}
	base := obj.GetResourceVersion()
	var v engine.SecretValue
	must(engine.RevealCommand, map[string]any{"ref": ref, "key": "password", "resourceVersion": base, "whole": true}, &v)
	if v.Value != "kind-pass-1" {
		t.Fatalf("revealed %q", v.Value)
	}

	change := map[string]any{"ref": ref, "resourceVersion": base, "data": map[string]string{"password": "kind-pass-2"},
		"from": map[string]string{"password": v.Value}}
	var review engine.SecretDataReview
	must(engine.SecretReviewDataCommand, change, &review)
	if !review.Accepted || review.Stale || len(review.Changes) != 1 || review.Changes[0].Key != "password" || review.Changes[0].NewBytes != 11 {
		t.Fatalf("review %+v", review)
	}
	for _, origin := range []engine.Origin{engine.OriginExternalAgent, engine.OriginCLI} {
		if err := runAs(origin, engine.SecretSaveDataCommand, change, nil); !errors.Is(err, engine.ErrSecretGUIOnly) {
			t.Errorf("a save from %s: %v", origin, err)
		}
	}
	var saved engine.SecretDataSaved
	must(engine.SecretSaveDataCommand, map[string]any{"ref": ref, "resourceVersion": base, "data": change["data"]}, &saved)
	cur := get()
	if cur.GetResourceVersion() != saved.ResourceVersion || value(cur, "password") != "kind-pass-2" || value(cur, "user") != "app" {
		t.Fatalf("after the save: version %s (saved %s), data %v", cur.GetResourceVersion(), saved.ResourceVersion, cur.Object["data"])
	}
	if _, kept := cur.GetAnnotations()["kubectl.kubernetes.io/last-applied-configuration"]; kept {
		t.Error("the last-applied annotation survived the save: H-103 is closed, update the docs")
	}

	// Another client changes another key: the review rebases onto it.
	cur.Object["data"].(map[string]any)["user"] = base64.StdEncoding.EncodeToString([]byte("other-client"))
	other, err := dyn.Resource(kindSecretsGVR).Namespace(secretDataNamespace).Update(ctx, cur, metav1.UpdateOptions{FieldManager: "other-client"})
	if err != nil {
		t.Fatal(err)
	}
	rebase := map[string]any{"ref": ref, "resourceVersion": saved.ResourceVersion, "data": map[string]string{"password": "kind-pass-3"},
		"from": map[string]string{"password": "kind-pass-2"}}
	review = engine.SecretDataReview{}
	must(engine.SecretReviewDataCommand, rebase, &review)
	if review.RebasedFrom != saved.ResourceVersion || review.BaseResourceVersion != other.GetResourceVersion() || !review.Accepted {
		t.Fatalf("a review over another key's change (version %s): %+v", other.GetResourceVersion(), review)
	}
	must(engine.SecretSaveDataCommand, map[string]any{"ref": ref, "resourceVersion": review.BaseResourceVersion, "data": rebase["data"]}, &saved)
	if cur = get(); value(cur, "password") != "kind-pass-3" || value(cur, "user") != "other-client" {
		t.Fatalf("the rebased save: %v", cur.Object["data"])
	}

	// The edited key changes on the cluster: the conflict, then the save over it.
	cur.Object["data"].(map[string]any)["password"] = base64.StdEncoding.EncodeToString([]byte("rotated"))
	if other, err = dyn.Resource(kindSecretsGVR).Namespace(secretDataNamespace).Update(ctx, cur, metav1.UpdateOptions{FieldManager: "other-client"}); err != nil {
		t.Fatal(err)
	}
	stale := map[string]any{"ref": ref, "resourceVersion": saved.ResourceVersion, "data": map[string]string{"password": "kind-pass-4"}}
	err = runAs(engine.OriginGUI, engine.SecretSaveDataCommand, stale, nil)
	var ce *engine.DataConflictError
	if !errors.As(err, &ce) || strings.Contains(err.Error(), "kind-pass") || strings.Contains(err.Error(), "rotated") {
		t.Fatalf("a save over another client's change of the key: %v", err)
	}
	if value(get(), "password") != "rotated" {
		t.Fatal("a refused save wrote")
	}
	if !testkit.WaitFor(ctx, func() bool {
		o, err := s.Lookup(ref)
		return err == nil && o.GetResourceVersion() == other.GetResourceVersion()
	}) {
		t.Fatal("the cache never saw the other client's change")
	}
	stale["resourceVersion"] = other.GetResourceVersion()
	must(engine.SecretSaveDataCommand, stale, nil)
	if value(get(), "password") != "kind-pass-4" {
		t.Fatal("the save over the newer version did not write")
	}

	// Keys added and removed (the owner's audit decision of 2026-10-16): the server accepts the dry
	// run of an edit that adds a key and removes one; the save is destructive and asks the
	// confirmer, naming the keys only; the Secret then holds the added value base64-encoded and not
	// the removed key. A key added meanwhile by another client is a conflict, never a rebase.
	var asked []engine.ConfirmRequest
	s.SetConfirmer(engine.ConfirmFunc(func(_ context.Context, req engine.ConfirmRequest) (bool, error) {
		asked = append(asked, req)
		return true, nil
	}))
	at := get().GetResourceVersion()
	if !testkit.WaitFor(ctx, func() bool { o, err := s.Lookup(ref); return err == nil && o.GetResourceVersion() == at }) {
		t.Fatal("the cache never saw the save")
	}
	keys := map[string]any{"ref": ref, "resourceVersion": at, "add": map[string]string{"api-token": "kind-token-1"}, "remove": []string{"user"}}
	review = engine.SecretDataReview{}
	must(engine.SecretReviewDataCommand, keys, &review)
	if !review.Accepted || len(review.Changes) != 2 || review.Changes[0] != (engine.SecretKeyChange{Key: "api-token", Change: engine.SecretKeyAdded, NewBytes: 12}) ||
		review.Changes[1] != (engine.SecretKeyChange{Key: "user", Change: engine.SecretKeyRemoved, Bytes: 12}) {
		t.Fatalf("the review of keys: %+v", review)
	}
	var keysSaved engine.SecretDataSaved
	must(engine.SecretSaveDataCommand, keys, &keysSaved)
	if len(asked) != 1 || asked[0].Danger != engine.DangerDestructive || asked[0].Detail != "Removes: user. Adds: api-token. Values: not shown." {
		t.Errorf("the confirmation of a removal: %+v", asked)
	}
	cur = get()
	if _, kept := cur.Object["data"].(map[string]any)["user"]; kept || value(cur, "api-token") != "kind-token-1" || value(cur, "password") != "kind-pass-4" {
		t.Fatalf("after the save of keys: %v", cur.Object["data"])
	}
	cur.Object["data"].(map[string]any)["extra"] = base64.StdEncoding.EncodeToString([]byte("other-client"))
	if other, err = dyn.Resource(kindSecretsGVR).Namespace(secretDataNamespace).Update(ctx, cur, metav1.UpdateOptions{FieldManager: "other-client"}); err != nil {
		t.Fatal(err)
	}
	appeared := map[string]any{"ref": ref, "resourceVersion": keysSaved.ResourceVersion, "add": map[string]string{"extra": "mine"}}
	if !testkit.WaitFor(ctx, func() bool {
		review = engine.SecretDataReview{}
		return runAs(engine.OriginGUI, engine.SecretReviewDataCommand, appeared, &review) == nil && review.CachedResourceVersion == other.GetResourceVersion()
	}) {
		t.Fatal("the review never saw the other client's key")
	}
	if !review.Stale || review.RebasedFrom != "" || len(review.Changes) != 1 || review.Changes[0].Change != engine.SecretKeyChanged {
		t.Errorf("an added key that appeared: %+v", review)
	}
}
