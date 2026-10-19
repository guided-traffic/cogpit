package kinds

import (
	"github.com/guided-traffic/cogpit/internal/model"
)

// ArgoSyncInitiator is the user name an operation Cogpit starts carries in initiatedBy: Argo CD's
// history and UI then say who synced (Cogpit does not know the Kubernetes user's name).
const ArgoSyncInitiator = "cogpit"

// ArgoSyncOperation builds the operation a Sync writes into an Application, as Argo CD's API
// server builds it for argocd app sync without options (D-36; Server.Sync of Argo CD 3.5.5, read
// 2026-10-16): the source the user confirmed, a copy of the spec's (source for one, sources for
// several: the controller then syncs it even if the spec changes before it runs, as it does for
// argocd app sync), the revision of each source as the spec targets it (revision for one source,
// revisions for several), prune as asked, the sync options and the retry strategy of the
// Application's own syncPolicy, and Cogpit as the initiator. No dry run, no strategy (the
// controller's default, apply), every resource.
func ArgoSyncOperation(app any, prune bool) map[string]any {
	sync := map[string]any{"prune": prune}
	if list := model.NestedSlice(app, fieldSpec, fieldArgoSources); len(list) > 0 {
		revs := make([]any, len(list))
		for i, s := range list {
			revs[i] = model.NestedString(s, fieldArgoTarget)
		}
		sync["revisions"] = revs
		sync[fieldArgoSources] = deepCopyJSON(list)
	} else {
		sync[fieldArgoRevision] = model.NestedString(app, fieldSpec, fieldArgoSource, fieldArgoTarget)
		if src := model.NestedMap(app, fieldSpec, fieldArgoSource); src != nil {
			sync[fieldArgoSource] = deepCopyJSON(src)
		}
	}
	if opts := model.NestedStrings(app, fieldSpec, fieldArgoSyncPol, "syncOptions"); len(opts) > 0 {
		list := make([]any, len(opts))
		for i, o := range opts {
			list[i] = o
		}
		sync["syncOptions"] = list
	}
	op := map[string]any{
		"initiatedBy": map[string]any{"username": ArgoSyncInitiator},
		"sync":        sync,
	}
	if retry := model.NestedMap(app, fieldSpec, fieldArgoSyncPol, "retry"); retry != nil {
		op["retry"] = deepCopyJSON(retry)
	}
	return op
}

// ArgoPrunable names the resources an Application's status marks requiresPruning: what a sync
// with prune deletes, as the controller last compared the Application with Git, the kind set apart
// from the name as every confirmation names an object (D-85, WF32): "Deployment → shop/old-worker".
func ArgoPrunable(app any) []string {
	var out []string
	for _, r := range model.NestedSlice(app, fieldArgoStatus, "resources") {
		if p, _ := model.NestedBool(r, "requiresPruning"); !p {
			continue
		}
		name := model.NestedString(r, "name")
		if ns := model.NestedString(r, "namespace"); ns != "" {
			name = ns + "/" + name
		}
		out = append(out, model.NestedString(r, idKind)+" → "+name)
	}
	return out
}

// deepCopyJSON copies JSON-shaped data (maps, slices, scalars, null), so that an operation never
// aliases the cached object it was built from. Anything else is left out; a null stays (a Helm
// value set to null removes a chart's default, so a copied source must keep it).
func deepCopyJSON(v any) any {
	c, _ := copyJSON(v)
	return c
}

// copyJSON is deepCopyJSON; ok is false for a value that is not JSON-shaped.
func copyJSON(v any) (any, bool) {
	switch t := v.(type) {
	case nil:
		return nil, true
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, e := range t {
			if c, ok := copyJSON(e); ok {
				out[k] = c
			}
		}
		return out, true
	case []any:
		out := make([]any, 0, len(t))
		for _, e := range t {
			if c, ok := copyJSON(e); ok {
				out = append(out, c)
			}
		}
		return out, true
	case string, bool, int64, float64, int, int32:
		return t, true
	}
	return nil, false
}
