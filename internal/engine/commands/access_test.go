package commands_test

import (
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/model"
)

// denyAccess denies the verbs it lists ("verb resource namespace") and allows every other one.
type denyAccess map[string]bool

func (d denyAccess) Allowed(namespace string, v engine.Verb) (bool, bool) {
	return !d[v.Verb+" "+v.Resource+" "+namespace], true
}

// swapAccess is the access reviews a test changes while the session runs. The harness's connector
// puts it on the backend before the engine publishes it, so a test never assigns a field of a live
// backend, which the engine's goroutines read; set changes what it denies under Allowed's lock.
// Denying nothing, it answers as no access reviews would: everything allowed.
type swapAccess struct {
	mu   sync.Mutex
	deny denyAccess
}

func (a *swapAccess) Allowed(namespace string, v engine.Verb) (bool, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.deny.Allowed(namespace, v)
}

func (a *swapAccess) set(deny denyAccess) {
	a.mu.Lock()
	a.deny = deny
	a.mu.Unlock()
}

// kinds.list reports per kind whether the user may list it, from the backend's Access: every kind
// while it denies nothing; a namespaced kind the user may not list everywhere stays unknown
// (listable somewhere, perhaps).
func TestKindsListListable(t *testing.T) {
	h := newHarness(t, seed(), engine.Options{})
	listable := func() map[string]*bool {
		var kinds []commands.KindInfo
		h.must(t, engine.OriginExternalAgent, "kinds.list", nil, &kinds)
		out := map[string]*bool{}
		for _, k := range kinds {
			out[k.Kind] = k.Listable
		}
		return out
	}
	for kind, l := range listable() {
		if l == nil || !*l {
			t.Errorf("%s: listable %v while nothing is denied, want true", kind, l)
		}
	}
	h.access.set(denyAccess{"list namespaces ": true, "list deployments ": true})
	got := listable()
	if l := got["namespaces"]; l == nil || *l {
		t.Errorf("namespaces: %v, want false", l)
	}
	if l := got["pods"]; l == nil || !*l {
		t.Errorf("pods: %v, want true", l)
	}
	if l := got["deployments"]; l != nil {
		t.Errorf("deployments: %v, want unknown", *l)
	}
}

// Edit is offered when the user may update the object, Delete when they may delete it; a denial
// greys them with the reason for every origin.
func TestEditAndDeleteFollowAccessReviews(t *testing.T) {
	h := newHarness(t, seed(), engine.Options{})
	ref := model.ObjectRef{Kind: "pods", Namespace: "shop", Name: "web-0"}
	waitTable(t, h.s, "pods", func(ts *engine.TableSnapshot) bool { return ts.Synced })
	h.access.set(denyAccess{"update pods shop": true, "delete pods shop": true})
	for _, id := range []string{"object.edit", "object.delete"} {
		err := h.run(t, engine.OriginGUI, id, map[string]any{"ref": ref}, nil)
		var dis *engine.DisabledError
		if !errors.As(err, &dis) || !strings.HasPrefix(dis.Reason, "not permitted: you may not ") {
			t.Errorf("%s: %v, want disabled as not permitted", id, err)
		}
	}
	other := model.ObjectRef{Kind: "pods", Namespace: "infra", Name: "web-0"}
	cmd, _ := h.s.Registry().Get("object.edit")
	if err := cmd.Enabled(h.s, cmd.ObjectParams(other, "")); err != nil {
		t.Errorf("Edit in another namespace: %v", err)
	}
}

// cluster.refreshAccess needs a cluster with access reviews: the demo cluster and backends
// without them have nothing to refresh.
func TestRefreshAccessNeedsAccessReviews(t *testing.T) {
	h := newHarness(t, seed(), engine.Options{})
	err := h.run(t, engine.OriginExternalAgent, "cluster.refreshAccess", nil, nil)
	var dis *engine.DisabledError
	if !errors.As(err, &dis) || !strings.Contains(dis.Reason, "no access reviews") {
		t.Errorf("refresh without access reviews: %v", err)
	}
	cmd, _ := h.s.Registry().Get("cluster.refreshAccess")
	if len(cmd.Verbs) != 2 || cmd.Verbs[0].Group != "authorization.k8s.io" {
		t.Errorf("verbs %+v", cmd.Verbs)
	}
}
