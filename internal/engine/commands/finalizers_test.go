package commands_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// The fake writer's side of Remove finalizers: each removal recorded with its UID and finalizers;
// failFinalizers, when set, is returned instead (the API server's failed test).

var _ engine.FinalizerWriter = (*fakeWriter)(nil)

var (
	finalizerMu    sync.Mutex
	finalizerCalls = map[*fakeWriter][]string{}
	finalizerFail  = map[*fakeWriter]error{}
)

func (w *fakeWriter) RemoveFinalizers(_ context.Context, gvr schema.GroupVersionResource, namespace, name, uid string, finalizers []string) error {
	finalizerMu.Lock()
	defer finalizerMu.Unlock()
	finalizerCalls[w] = append(finalizerCalls[w], gvr.Resource+" "+namespace+"/"+name+" "+uid+" "+strings.Join(finalizers, ","))
	return finalizerFail[w]
}

func removals(w *fakeWriter) []string {
	finalizerMu.Lock()
	defer finalizerMu.Unlock()
	return slices.Clone(finalizerCalls[w])
}

// beingDeleted is a pod of the fixture being deleted, held by finalizers.
func beingDeleted(ns, name string, fins ...string) *unstructured.Unstructured {
	pod := testkit.Pod(ns, name)
	pod.SetFinalizers(fins)
	ts := pod.GetCreationTimestamp()
	ts.Time = time.Date(2026, 10, 19, 10, 0, 0, 0, time.UTC)
	pod.SetDeletionTimestamp(&ts)
	return pod
}

// finalizerHarness serves a pod shop/web-0 being deleted with two finalizers, the pods listed.
func finalizerHarness(t *testing.T) (*harness, model.ObjectRef) {
	t.Helper()
	src := seed()
	src.Set(testkit.PodsGVR, beingDeleted("shop", "web-0", "example.com/x", "example.com/y"))
	h := newHarness(t, src, engine.Options{})
	if _, err := h.s.Objects(context.Background(), "pods", nil); err != nil {
		t.Fatal(err)
	}
	return h, model.ObjectRef{Kind: "pods", Namespace: "shop", Name: "web-0"}
}

// TestRemoveFinalizersConfirmsWhatItRemoves: Destructive, confirmed for the window too; the
// confirmation names the object and, as rows, the finalizers it removes (lost) and the cleanup it
// skips (warning); the patch carries the confirmed UID and exactly those finalizers.
func TestRemoveFinalizersConfirmsWhatItRemoves(t *testing.T) {
	h, ref := finalizerHarness(t)
	var asked []engine.ConfirmRequest
	h.s.SetConfirmer(engine.ConfirmFunc(func(_ context.Context, req engine.ConfirmRequest) (bool, error) {
		asked = append(asked, req)
		return true, nil
	}))
	var res engine.RemoveFinalizersResult
	h.must(t, engine.OriginGUI, engine.RemoveFinalizersCommand, map[string]any{"ref": ref}, &res)
	if len(asked) != 1 {
		t.Fatalf("asked %d times", len(asked))
	}
	req := asked[0]
	if req.Danger != engine.DangerDestructive || req.Context != "test" || len(req.Objects) != 1 || req.Objects[0].Name != "web-0" ||
		!slices.Equal(req.Namespaces, []string{"shop"}) || req.Undoable {
		t.Errorf("request %+v", req)
	}
	want := []engine.Effect{
		{Name: engine.EffectRemovesFinalizers, Value: "example.com/x\nexample.com/y", Tone: engine.EffectLost},
		{Name: "Skips", Value: "the cleanup of the controllers that own them (a cloud disk or a DNS record may stay)", Tone: engine.EffectWarning},
	}
	if !slices.Equal(req.Effects, want) || !strings.Contains(req.Message(), "example.com/x; example.com/y") {
		t.Errorf("effects %+v, message %q", req.Effects, req.Message())
	}
	if got := removals(h.writer); !slices.Equal(got, []string{"pods shop/web-0 uid-shop-web-0 example.com/x,example.com/y"}) {
		t.Errorf("removals %v", got)
	}
	if res.Action != "finalizers removed" || !slices.Equal(res.Removed, []string{"example.com/x", "example.com/y"}) {
		t.Errorf("result %+v", res)
	}
}

// TestRemoveFinalizersRefusals: not applicable to an object not being deleted, one without
// finalizers, a namespace; disabled for finalizers that are not the object's; an agent's call waits
// for the user; a finalizer that changed while the dialog was open, and the API server's failed
// test, are refused as changed targets.
func TestRemoveFinalizersRefusals(t *testing.T) {
	h, ref := finalizerHarness(t)
	cmd, _ := h.s.Registry().Get(engine.RemoveFinalizersCommand)
	notApplicable := map[string]model.ObjectRef{
		"not being deleted": {Kind: "pods", Namespace: "shop", Name: "web-1"},
		"a namespace":       {Kind: "namespaces", Name: "shop"},
	}
	for what, r := range notApplicable {
		if cmd.Applicable(h.s, cmd.ObjectParams(r, "")) {
			t.Errorf("%s: applicable (%v)", what, cmd.Enabled(h.s, cmd.ObjectParams(r, "")))
		}
	}
	if err := h.run(t, engine.OriginGUI, engine.RemoveFinalizersCommand, map[string]any{"ref": ref, "finalizers": []string{"example.com/y", "example.com/x"}}, nil); !errors.Is(err, engine.ErrTargetsChanged) {
		t.Errorf("finalizers in another order: %v", err)
	}

	// An agent's call waits for the user; declined, nothing is sent.
	h.s.SetConfirmer(engine.ConfirmFunc(func(context.Context, engine.ConfirmRequest) (bool, error) { return false, nil }))
	if err := h.run(t, engine.OriginExternalAgent, engine.RemoveFinalizersCommand, map[string]any{"ref": ref}, nil); !errors.Is(err, engine.ErrDeclined) {
		t.Errorf("agent declined: %v", err)
	}

	// A finalizer added while the dialog was open: the cache has it before Run, which refuses.
	h.s.SetConfirmer(engine.ConfirmFunc(func(ctx context.Context, _ engine.ConfirmRequest) (bool, error) {
		h.src.Set(testkit.PodsGVR, beingDeleted("shop", "web-0", "example.com/x", "example.com/y", "example.com/z"))
		testkit.WaitFor(ctx, func() bool {
			obj, err := h.s.Lookup(ref)
			return err == nil && len(obj.GetFinalizers()) == 3
		})
		return true, nil
	}))
	if err := h.run(t, engine.OriginGUI, engine.RemoveFinalizersCommand, map[string]any{"ref": ref}, nil); !errors.Is(err, engine.ErrTargetsChanged) {
		t.Errorf("changed while confirming: %v", err)
	}
	// The API server's failed test (the change reached it, not the cache).
	h.s.SetConfirmer(engine.ConfirmFunc(func(context.Context, engine.ConfirmRequest) (bool, error) { return true, nil }))
	finalizerMu.Lock()
	finalizerFail[h.writer] = cluster.ErrFinalizersChanged
	finalizerMu.Unlock()
	if err := h.run(t, engine.OriginGUI, engine.RemoveFinalizersCommand, map[string]any{"ref": ref}, nil); !errors.Is(err, engine.ErrTargetsChanged) {
		t.Errorf("failed test: %v", err)
	}
	if got := removals(h.writer); len(got) != 1 || !strings.HasSuffix(got[0], "example.com/x,example.com/y,example.com/z") {
		t.Errorf("removals %v (only the last, with the finalizers it confirmed)", got)
	}
}

// TestRemoveFinalizersOfAKindWithoutAProfile: a kind Cogpit has no view of (resolved through
// discovery) is checked when it runs, against its cached object.
func TestRemoveFinalizersOfAKindWithoutAProfile(t *testing.T) {
	src := seed()
	cm := beingDeleted("shop", "settings", "example.com/x")
	cm.SetAPIVersion("v1")
	cm.SetKind("ConfigMap")
	src.Set(configMapsGVR, cm)
	src.Set(configMapsGVR, testkit.Object("v1", "ConfigMap", "shop", "live", map[string]any{"metadata": map[string]any{"finalizers": []any{"example.com/x"}}}))
	h := newHarness(t, src, engine.Options{})
	h.s.SetConfirmer(engine.ConfirmFunc(func(context.Context, engine.ConfirmRequest) (bool, error) { return true, nil }))
	ref := model.ObjectRef{Kind: "configmaps", Namespace: "shop", Name: "settings"}
	h.must(t, engine.OriginGUI, engine.RemoveFinalizersCommand, map[string]any{"ref": ref, "finalizers": []string{"example.com/x"}}, nil)
	if got := removals(h.writer); !slices.Equal(got, []string{"configmaps shop/settings uid-shop-settings example.com/x"}) {
		t.Errorf("removals %v", got)
	}
	live := model.ObjectRef{Kind: "configmaps", Namespace: "shop", Name: "live"}
	if err := h.run(t, engine.OriginGUI, engine.RemoveFinalizersCommand, map[string]any{"ref": live, "finalizers": []string{"example.com/x"}}, nil); !errors.Is(err, engine.ErrNotBeingDeleted) {
		t.Errorf("an object not being deleted: %v", err)
	}
}
