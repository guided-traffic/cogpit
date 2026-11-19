package engine

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/guided-traffic/cogpit/internal/cache"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// discoveryConnector is crdConnector with API discovery: discover answers Backend.Discovery and
// counts its calls.
type discoveryConnector struct {
	src      cache.Source
	discover func(context.Context) (map[schema.GroupVersionResource]bool, error)
	calls    atomic.Int32
}

func (c *discoveryConnector) Contexts() ([]ContextInfo, error) {
	return []ContextInfo{{Name: "test"}}, nil
}

func (c *discoveryConnector) Connect(context.Context, string) (*Backend, error) {
	ch := cache.New(c.src, cache.Options{WarmPeriod: time.Minute})
	return &Backend{
		Context: "test", Namespace: "default", Cache: ch, Profiles: testProfiles(), CustomResources: true, Close: ch.Close,
		Discovery: func(ctx context.Context) (map[schema.GroupVersionResource]bool, error) {
			c.calls.Add(1)
			return c.discover(ctx)
		},
	}, nil
}

// refusedDefinitions is a source whose definitions the API server refuses to list, as it does a
// namespace-limited user's.
func refusedDefinitions() cache.Source {
	mem := testkit.NewMemorySource()
	mem.Set(testkit.PodsGVR, testkit.Pod("default", "web-0"))
	return testkit.RefusingSource{Inner: mem, Refuse: map[schema.GroupVersionResource]error{
		crdsGVR: apierrors.NewForbidden(crdsGVR.GroupResource(), "", errors.New(`User "dev" cannot list resource "customresourcedefinitions"`)),
	}}
}

// servedGitOps is what discovery names on a cluster with Flux's Kustomizations and Git
// repositories, Argo CD's Applications, a generic kind and Flux's image policies at a version no
// curated profile reads.
func servedGitOps() map[schema.GroupVersionResource]bool {
	return map[schema.GroupVersionResource]bool{
		{Group: "kustomize.toolkit.fluxcd.io", Version: "v1", Resource: "kustomizations"}: true,
		{Group: "source.toolkit.fluxcd.io", Version: "v1", Resource: "gitrepositories"}:   true,
		{Group: "argoproj.io", Version: "v1alpha1", Resource: "applications"}:             true,
		{Group: orderGroup, Version: "v1", Resource: "orders"}:                            true,
		{Group: "image.toolkit.fluxcd.io", Version: "v9", Resource: "imagepolicies"}:      true,
		{Version: "v1", Resource: "pods"}:                                                 true,
	}
}

// TestCuratedKindsFromDiscovery (A90): a user who may not list the definitions gets the curated
// kinds the cluster serves by API discovery, with their curated profiles, and Argo CD's settings
// views with its Applications; a generic kind and a curated kind at a version its profile does not
// read stay invisible; the GitOps dashboards no longer say they cannot tell.
func TestCuratedKindsFromDiscovery(t *testing.T) {
	c := &discoveryConnector{src: refusedDefinitions(), discover: func(context.Context) (map[schema.GroupVersionResource]bool, error) {
		return servedGitOps(), nil
	}}
	s := New(Options{Connector: c, SnapshotInterval: 5 * time.Millisecond})
	t.Cleanup(s.Close)
	connectTest(t, s)
	b := s.Backend()
	ks := "kustomizations.kustomize.toolkit.fluxcd.io"
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if !testkit.WaitFor(ctx, func() bool { return hasKind(s, ks) }) {
		// What the custom kinds' loop last saw, for a failure under load.
		set := b.custom.set.Load()
		h, errMsg := b.custom.current()
		t.Fatalf("timed out waiting for the discovered kinds: discovery asked %d times; the set: synced %v, err %q, "+
			"discovered %v, %d kinds; now: refused %v, synced %v, err %q, handle err %v",
			c.calls.Load(), set.synced, set.err, set.discovered, len(set.profiles), b.custom.refused(), h.HasSynced(), errMsg, h.Err())
	}
	curated := map[string]*model.KindProfile{}
	for _, p := range kinds.Curated() {
		curated[p.Key] = p
	}
	for _, key := range []string{ks, "gitrepositories.source.toolkit.fluxcd.io", kinds.ArgoApplicationsKey} {
		if p, ok := b.Profile(key); !ok || p != curated[key] {
			t.Errorf("%s: %v, want the curated profile", key, ok)
		}
	}
	for _, settings := range kinds.ArgoSettings() {
		if !hasKind(s, settings.Key) {
			t.Errorf("Argo CD's %s did not come with its Applications", settings.Key)
		}
	}
	for _, key := range []string{orderKey, "imagepolicies.image.toolkit.fluxcd.io", "helmreleases.helm.toolkit.fluxcd.io"} {
		if hasKind(s, key) {
			t.Errorf("%s offered: generic kinds, kinds at a version no profile reads and kinds not served stay invisible", key)
		}
	}
	if _, _, err := s.Kind(orderKey); !errors.Is(err, ErrUnknownKind) || !strings.Contains(err.Error(), "custom resource kinds are unavailable") {
		t.Errorf("a generic kind: %v", err)
	}
	for _, what := range []string{"Flux kinds", "Argo CD Applications"} {
		if why := b.customKindsUnknown(what); why != "" {
			t.Errorf("%s: %q, want known from discovery", what, why)
		}
	}
	if n := c.calls.Load(); n == 0 {
		t.Error("discovery never asked")
	}
}

// TestCuratedKindsWhenDiscoveryFails (A90): a discovery that fails adds nothing, and the
// dashboards keep saying why they cannot tell.
func TestCuratedKindsWhenDiscoveryFails(t *testing.T) {
	c := &discoveryConnector{src: refusedDefinitions(), discover: func(context.Context) (map[schema.GroupVersionResource]bool, error) {
		return nil, errors.New("discovery: the server is gone")
	}}
	s := New(Options{Connector: c, SnapshotInterval: 5 * time.Millisecond})
	t.Cleanup(s.Close)
	connectTest(t, s)
	b := s.Backend()
	waitFor(t, "the forbidden list and a discovery", func() bool {
		set := b.customSet()
		return set != nil && set.err != "" && c.calls.Load() > 0
	})
	if len(b.CustomProfiles()) != 0 {
		t.Errorf("custom kinds without definitions and discovery: %d", len(b.CustomProfiles()))
	}
	if why := b.customKindsUnknown("Flux kinds"); !strings.HasPrefix(why, "Cogpit cannot tell whether the cluster serves Flux kinds") {
		t.Errorf("the dashboard says %q", why)
	}
}

// TestCuratedKindsNotDiscoveredWithTheDefinitions (A90): a user who may list the definitions is
// never asked discovery for them: the definitions name the kinds.
func TestCuratedKindsNotDiscoveredWithTheDefinitions(t *testing.T) {
	src := testkit.NewMemorySource()
	src.Set(crdsGVR, orderCRD())
	c := &discoveryConnector{src: src, discover: func(context.Context) (map[schema.GroupVersionResource]bool, error) {
		return servedGitOps(), nil
	}}
	s := New(Options{Connector: c, SnapshotInterval: 5 * time.Millisecond})
	t.Cleanup(s.Close)
	connectTest(t, s)
	waitFor(t, "the defined kind", func() bool { return hasKind(s, orderKey) })
	time.Sleep(50 * time.Millisecond) // rebuilds after the first
	if n := c.calls.Load(); n != 0 || hasKind(s, "kustomizations.kustomize.toolkit.fluxcd.io") {
		t.Errorf("discovery asked %d times with the definitions listed", n)
	}
}
