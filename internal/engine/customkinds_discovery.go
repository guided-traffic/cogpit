package engine

import (
	"context"
	"time"

	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/guided-traffic/cogpit/internal/model/kinds"
)

// Curated kinds from API discovery (A90, the owner's decision): a user who may not list the custom
// resource definitions (a ClusterRole is needed for that, so as a rule a namespace-limited user)
// had no Flux or Argo CD kinds at all. Discovery, which every authenticated user may read
// (system:discovery), names the resources the cluster serves; a kind Cogpit has a curated profile
// for (kinds.Curated: Flux's thirteen, Argo CD's three, the Vertical Pod Autoscalers) that the
// cluster serves at a version the profile reads gets that profile, as its definition would give it,
// and Argo CD's settings views come with its Applications as they do with the definitions. Generic
// custom resources stay invisible: discovery names their resources but not their printer columns,
// and the owner kept them out. Flux's HelmCharts have no curated profile, so Reconcile with Source
// of a Helm release made from spec.chart says the cluster does not serve them, for such a user.
//
// Discovery is asked while the definitions cannot be listed, at each rebuild of the custom kinds:
// after the refused list that ends their watch (customKinds.drop), and again when a refresh of the
// access reviews takes the watch again (retake). The connection's discovery result is cached, in
// memory and in the disk cache (D-65), for cluster.ServedResourcesTTL, since no list of the
// definitions confirms it, and a refresh of the access reviews asks the server again (retake). A
// kind installed while the definitions stay refused shows after the next refresh, or at a connect
// once the cached answer is older than that.

// discoveryTimeout bounds one discovery of the curated kinds.
const discoveryTimeout = 15 * time.Second

// discovery returns the backend's discovery: Backend.Discovery, else its connection's; nil without
// either (the demo cluster, tests without a connection).
func (b *Backend) discovery() func(context.Context) (map[schema.GroupVersionResource]bool, error) {
	switch {
	case b.Discovery != nil:
		return b.Discovery
	case b.Conn != nil:
		return b.Conn.ServedResources
	}
	return nil
}

// discoverCurated adds to next the curated kinds the cluster serves by API discovery, keeping an
// earlier build's entry for a kind that did not change. It reports whether discovery answered; a
// discovery that fails adds nothing, and the custom kinds keep saying why they are unknown.
func (ck *customKinds) discoverCurated(next map[string]customEntry) bool {
	discover := ck.b.discovery()
	if discover == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), discoveryTimeout)
	defer cancel()
	go func() { // the backend closing ends the request
		select {
		case <-ck.quit:
			cancel()
		case <-ctx.Done():
		}
	}()
	served, err := discover(ctx)
	if err != nil {
		ck.s.log.Warn("engine: discovering the curated kinds failed", "context", ck.b.Context, "err", err)
		return false
	}
	for _, k := range curatedServed(served) {
		key := k.Key()
		if _, taken := next[key]; taken || ck.b.builtinProfile(key) != nil {
			continue
		}
		if old, ok := ck.entries[key]; ok && old.kind.Equal(k) {
			next[key] = old
			continue
		}
		next[key] = customEntry{kind: k, profile: kinds.ProfileFor(k)}
	}
	return true
}

// curatedServed returns the curated kinds served at a version their profile reads: its own, else
// the first of its fallback versions the cluster serves. Discovery names one version per resource
// (the group's preferred one when it serves the resource).
func curatedServed(served map[schema.GroupVersionResource]bool) []kinds.CustomKind {
	var out []kinds.CustomKind
	for _, p := range kinds.Curated() {
		for _, v := range append([]string{p.Version}, p.FallbackVersions...) {
			if served[schema.GroupVersionResource{Group: p.Group, Version: v, Resource: p.Resource}] {
				out = append(out, kinds.CustomKind{Group: p.Group, Version: v, Plural: p.Resource, Kind: p.Kind, Namespaced: p.Namespaced})
				break
			}
		}
	}
	return out
}
