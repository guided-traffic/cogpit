package commands_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// replacingWriter answers every restart and scale as the API server does for a patch whose UID is
// not the object's: the object was created again since the cache last saw it.
type replacingWriter struct{ *fakeWriter }

func (w replacingWriter) RestartDeployment(context.Context, string, string, string) error {
	return fmt.Errorf("restart deployment: %w", cluster.ErrReplaced)
}

func (w replacingWriter) ScaleWorkload(context.Context, string, string, string, string, int32) error {
	return fmt.Errorf("scale: %w", cluster.ErrReplaced)
}

// TestRestartAndScaleCarryTheUID: Restart and Scale send the UID they act on in their patch, for
// every origin: the confirmed one when the user confirmed (Restart always; Scale to zero; an
// agent's Scale the user accepted, which now records the UIDs the cache holds), else the cache's.
// An object recreated while an agent's Scale waited for the user is refused, and so is one the
// cache had not seen yet, which the API server refuses (cluster.ErrReplaced).
func TestRestartAndScaleCarryTheUID(t *testing.T) {
	h := newWorkloadHarness(t, nil, nil, engine.Options{})
	c := &confirmations{answer: true}
	h.s.SetConfirmer(c)
	deploy, sts := actionRef("deployments", "shop", "web"), actionRef("statefulsets", "shop", "db")
	cached := func(kind, ns, name string) string {
		t.Helper()
		obj, err := h.s.Lookup(actionRef(kind, ns, name))
		if err != nil {
			t.Fatal(err)
		}
		return string(obj.GetUID())
	}
	webUID, dbUID := cached("deployments", "shop", "web"), cached("statefulsets", "shop", "db")
	if webUID == "" || dbUID == "" {
		t.Fatal("fixture objects without UIDs")
	}

	h.must(t, engine.OriginGUI, "deployment.restart", map[string]any{"ref": deploy}, nil) // confirmed
	h.must(t, engine.OriginGUI, "statefulset.scale", scaleTo(sts, 3), nil)                // not confirmed
	h.must(t, engine.OriginExternalAgent, "statefulset.scale", scaleTo(sts, 2), nil)      // accepted by the user
	if got := h.writer.UIDs(); !slices.Equal(got, []string{webUID, dbUID, dbUID}) {
		t.Errorf("UIDs sent %q, want %q", got, []string{webUID, dbUID, dbUID})
	}
	if req := c.last(t); req.Danger != engine.DangerMutating || !slices.Equal(req.UIDs, []string{dbUID}) {
		t.Errorf("the agent's scale was confirmed as %s with UIDs %q", req.Danger, req.UIDs)
	}

	// An agent's scale whose stateful set is created again while the user looks at the request.
	c.during = func(context.Context) {
		obj := testkit.Object("apps/v1", "StatefulSet", "shop", "db", nil)
		obj.SetUID("uid-again")
		h.src.Set(statefulSetsGVR, obj)
		waitRecreated(t, h, sts)
	}
	n := len(h.writer.Calls())
	if err := h.run(t, engine.OriginExternalAgent, "statefulset.scale", scaleTo(sts, 5), nil); !errors.Is(err, engine.ErrTargetsChanged) {
		t.Errorf("an accepted scale of a recreated stateful set: %v", err)
	}
	if calls := h.writer.Calls(); len(calls) != n {
		t.Errorf("writes %v", calls[n:])
	}

	// The cache has not seen the new object yet: the API server refuses the patch.
	r := newWorkloadHarness(t, nil, replacingWriter{&fakeWriter{}}, engine.Options{Confirmer: &confirmations{answer: true}})
	if err := r.run(t, engine.OriginGUI, "deployment.restart", map[string]any{"ref": deploy}, nil); !errors.Is(err, engine.ErrTargetsChanged) || !errors.Is(err, cluster.ErrReplaced) {
		t.Errorf("restart refused by the API server: %v", err)
	}
	if err := r.run(t, engine.OriginGUI, "statefulset.scale", scaleTo(sts, 0), nil); !errors.Is(err, engine.ErrTargetsChanged) {
		t.Errorf("scale refused by the API server: %v", err)
	}
}
