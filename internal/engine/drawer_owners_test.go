package engine

import (
	"context"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// TestDrawerLinksCustomOwners: a drawer's Controlled By links an owner that is a custom resource
// to it, through the definitions the custom kinds watch (on a real cluster, 2026-10-16, a
// StatefulSet owned by a Prometheus, a Deployment by a CephCluster, a pod by cert-manager's
// Challenge were plain text); the definitions' index is every drawer's on a cluster with custom
// kinds, none's on one without, and never a virtual kind's.
func TestDrawerLinksCustomOwners(t *testing.T) {
	src := testkit.NewMemorySource()
	src.Set(crdsGVR, orderCRD())
	src.Set(ordersV1, order("shop", "tls-1", "valid"))
	solver := testkit.Pod("shop", "solver")
	solver.SetOwnerReferences([]metav1.OwnerReference{{APIVersion: "acme.cert-manager.io/v1", Kind: "Order", Name: "tls-1", UID: "uid-order"}})
	src.Set(testkit.PodsGVR, solver)
	definitions, ok := kinds.ByKey(kinds.KeyCustomResourceDefinitions)
	if !ok {
		t.Fatal("no profile of the definitions")
	}
	s := newCRDSession(t, src, definitions)
	waitFor(t, "the Order kind", func() bool { return hasKind(s, orderKey) })

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var owner *model.DetailItem
	waitFor(t, "the pod's owner linked", func() bool {
		m, err := s.DetailWait(ctx, model.ObjectRef{Context: "test", Kind: "pods", Namespace: "shop", Name: "solver"})
		if err != nil || len(m.Sections) == 0 {
			return false
		}
		for i, it := range m.Sections[0].Items {
			if it.Name == "Controlled By" {
				owner = &m.Sections[0].Items[i]
			}
		}
		return owner != nil && owner.Link != nil
	})
	if want := (model.ObjectRef{Kind: orderKey, Namespace: "shop", Name: "tls-1"}); owner.Value != "Order tls-1" || *owner.Link != want {
		t.Errorf("Controlled By %+v, want a link to %+v", owner, want)
	}

	b := s.Backend()
	p, _ := b.Profile("pods")
	if needs := drawerNeeds(b, p, false); len(needs) == 0 || needs[len(needs)-1].ID() != kinds.CustomKindsByGroupKind.ID() {
		t.Errorf("a drawer on a cluster with custom kinds reads %v", needs)
	}
	plain := &Backend{Profiles: testProfiles()}
	if needs := drawerNeeds(plain, p, false); len(needs) != len(p.DrawerNeeds()) {
		t.Errorf("a drawer on a cluster without custom kinds reads %v", needs)
	}
	virtual := &model.KindProfile{Key: "derived", Derive: func(objs []*unstructured.Unstructured) []*unstructured.Unstructured { return objs }}
	if needs := drawerNeeds(b, virtual, false); len(needs) != 0 {
		t.Errorf("a virtual kind's drawer reads %v", needs)
	}
}
