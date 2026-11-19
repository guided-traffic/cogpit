package kinds

import (
	"fmt"
	"slices"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/model"
)

// The workloads a GitOps object deploys, for its merged logs (A81, the owner's decision: an
// application's logs in one tab; engine/grouplogs.go): what its controller reports in its status,
// read from the cached object, no I/O. Only the kinds whose pods' logs a tab shows count
// (Deployments, Stateful Sets, Daemon Sets, Replica Sets, Jobs); everything else an object deploys
// is left out.

// workloadKindKeys maps a workload's API group and Kind to its kind key.
var workloadKindKeys = map[string]string{
	"apps/Deployment": keyDeployments, "apps/StatefulSet": keyStatefulSets, "apps/DaemonSet": keyDaemonSets,
	"apps/ReplicaSet": keyReplicaSets, "batch/Job": keyJobs,
}

// workloadKey returns the kind key of a workload by its API group and Kind, or "".
func workloadKey(group, kind string) string { return workloadKindKeys[group+"/"+kind] }

// ArgoAppElsewhere returns the cluster an Argo CD Application deploys to when it is not the
// cluster Argo CD runs in (spec.destination.server, else its name), "" when it is: a remote
// destination's workloads are not in the connected cluster.
func ArgoAppElsewhere(app *unstructured.Unstructured) string {
	server := model.NestedString(app, fieldSpec, fieldArgoDest, "server")
	name := model.NestedString(app, fieldSpec, fieldArgoDest, fieldName)
	switch {
	case server != "" && server != argoInClusterServer:
		return server
	case server == "" && name != "" && name != argoInClusterName:
		return name
	}
	return ""
}

// ArgoAppWorkloads returns the workloads an Argo CD Application manages, as its status.resources
// lists them (Argo CD's application controller writes every managed resource there, with its
// group, kind, namespace and name), in that order; none when it deploys to another cluster.
func ArgoAppWorkloads(app *unstructured.Unstructured) []model.ObjectRef {
	if ArgoAppElsewhere(app) != "" {
		return nil
	}
	var out []model.ObjectRef
	for _, r := range model.NestedSlice(app, "status", "resources") {
		key := workloadKey(model.NestedString(r, "group"), model.NestedString(r, idKind))
		ns, name := model.NestedString(r, "namespace"), model.NestedString(r, fieldName)
		if key == "" || ns == "" || name == "" {
			continue
		}
		out = appendRef(out, model.ObjectRef{Kind: key, Namespace: ns, Name: name})
	}
	return out
}

// FluxInventoryWorkloads returns the workloads a Flux Kustomization applied, as its
// status.inventory lists them: entries whose id is "<namespace>_<name>_<group>_<kind>" (Flux's
// ObjMetadata, fluxcd/pkg/ssa), in that order. Kubernetes names hold no underscore, so the id
// splits at them.
func FluxInventoryWorkloads(ks *unstructured.Unstructured) []model.ObjectRef {
	var out []model.ObjectRef
	for _, e := range model.NestedSlice(ks, "status", "inventory", "entries") {
		ref, err := parseInventoryID(model.NestedString(e, "id"))
		if err != nil {
			continue
		}
		out = appendRef(out, ref)
	}
	return out
}

// parseInventoryID reads a Flux inventory entry's id as a workload's reference.
func parseInventoryID(id string) (model.ObjectRef, error) {
	parts := strings.Split(id, "_")
	if len(parts) != 4 {
		return model.ObjectRef{}, fmt.Errorf("inventory id %q: not <namespace>_<name>_<group>_<kind>", id)
	}
	key := workloadKey(parts[2], parts[3])
	if key == "" || parts[0] == "" || parts[1] == "" {
		return model.ObjectRef{}, fmt.Errorf("inventory id %q: not a namespaced workload", id)
	}
	return model.ObjectRef{Kind: key, Namespace: parts[0], Name: parts[1]}, nil
}

// appendRef appends a reference not listed yet.
func appendRef(refs []model.ObjectRef, r model.ObjectRef) []model.ObjectRef {
	if slices.ContainsFunc(refs, r.Same) {
		return refs
	}
	return append(refs, r)
}
