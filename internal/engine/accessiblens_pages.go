package engine

import (
	"strings"

	"github.com/guided-traffic/cogpit/internal/cache"
	"github.com/guided-traffic/cogpit/internal/model"
)

// The pages that read custom kinds (the Flux and Argo CD dashboards, D-34, D-36) for a
// namespace-limited user (G3, architecture §5), each with workers of its own. The open page waits for its kinds' plans as a
// view does (acquireForPage: nothing while the access reviews are out), its key tells a pending
// plan apart (pageReadKey), and an agent's read waits for the plans and follows a refusal
// (openFollowing), as the Overviews' do.

// acquireForPage is an open page's handle on one of its kinds: as the kind's plan says, and
// nothing while the plan is pending (cache.Pending), as the view holds nothing then, so that a
// user who may not list the kind in all namespaces is never sent a read there that the access
// reviews would have refused. The page starts again once they answer (pageReadKey).
func (b *Backend) acquireForPage(p *model.KindProfile) *cache.Handle {
	return b.acquireForView(p, b.namespacePlan(p))
}

// customKindsUnknown says why a page of custom kinds (the Flux and Argo CD dashboards) cannot tell
// whether the cluster serves what (its kinds, "Flux kinds"): the user may not list the custom
// resource definitions (a namespace-limited user, as a rule, whose Role names the kinds but not
// their definitions), or they are still being listed; "" when the custom kinds are known. Such a
// page never says the cluster serves none.
func (b *Backend) customKindsUnknown(what string) string {
	set := b.customSet()
	switch {
	case set == nil:
		return ""
	case set.err != "" && set.discovered: // API discovery told (A90, customkinds_discovery.go)
		return ""
	case set.err != "":
		return "Cogpit cannot tell whether the cluster serves " + what + ": the custom resource definitions cannot be listed (" + set.err + ")."
	case !set.synced:
		return "The cluster's custom resource definitions are still being listed."
	}
	return ""
}

// mayNotList is what a page says of a kind the access reviews say the user may not list in any
// namespace it would read it in (Backend.Listable): "Cogpit may not list
// kustomizations.kustomize.toolkit.fluxcd.io", as the integrations' states name such a kind (D-177).
func mayNotList(p *model.KindProfile) string {
	return "Cogpit may not list " + p.GVR().GroupResource().String()
}

// deniedKind reports whether the access reviews say the user may not list a kind where a reader
// would read it: nowhere (Listable), or in none of the namespaces its plan names.
func (b *Backend) deniedKind(p *model.KindProfile) bool {
	if listable, known := b.Listable(p, nil); known && !listable {
		return true
	}
	plan := b.namespacePlan(p)
	return plan.perNamespace && len(plan.watch) == 0
}

// pageReadKey is how a page reads its kinds through acquireForPage: per kind its plan's key
// (pending, all, or the namespaces), or "unlistable" for a kind the access reviews say the user may
// not list (deniedKind: the page leaves it out or names it, mayNotList), so that a page whose
// kinds' plans change starts again. Kinds the cluster does not serve are left out. The Overviews
// keep it too (A98).
func (b *Backend) pageReadKey(keys []string) string {
	var sb strings.Builder
	for _, key := range keys {
		p, ok := b.Profile(key)
		if !ok {
			continue
		}
		read := b.namespacePlan(p).key()
		if b.deniedKind(p) {
			read = "unlistable"
		}
		sb.WriteString(key)
		sb.WriteByte('=')
		sb.WriteString(read)
		sb.WriteByte('|')
	}
	return sb.String()
}
