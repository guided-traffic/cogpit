package kinds

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/model"
)

// A workload's Pods in its drawer (D-40, decided by the owner 2026-10-14): the drawers of
// Deployments, Replica Sets, Stateful Sets, Daemon Sets, Jobs and Cron Jobs end with the node
// drawer's Pods table (nodeDetailWith) without its Namespace column, which a workload's pods share
// (the owner's): Name, the warning icon, Node, Ready, CPU, Memory, Status, sortable, CPU and Memory
// filled by the engine from the metrics while the drawer shows it (engine/drawerpods.go). Inferred:
// Lens's workload drawers are not captured.
//
// The pods are found by their controlling owner's UID, through the related-objects indexes below,
// which the profiles name in DetailNeeds, not Needs: only an open drawer holds them, so the
// workload lists do not rebuild their rows on every pod change (G6). A Deployment's pods are those
// of every replica set it controls, old ones during a rollout included; a Cron Job's those of every
// job it controls; the others' those they control themselves. An owner reference counts only in
// its object's own namespace, as Kubernetes's garbage collector reads it: the indexes file an
// object under "<namespace>/<owner UID>".

const (
	// The workload kinds' keys.
	keyDeployments  = "deployments"
	keyStatefulSets = "statefulsets"
	keyDaemonSets   = "daemonsets"
	keyReplicaSets  = "replicasets"
	keyJobs         = "jobs"
	// indexByOwner names the indexes by controlling owner.
	indexByOwner = "byOwner"
)

var (
	// PodsByOwner indexes pods by their controlling owner, as "<namespace>/<owner UID>": a replica
	// set's, stateful set's, daemon set's or job's pods.
	PodsByOwner = model.RelatedIndex{Kind: podsResource, Name: indexByOwner, Keys: controllerKeys}
	// ReplicaSetsByOwner indexes replica sets by their controlling owner, as "<namespace>/<owner
	// UID>": a Deployment's replica sets.
	ReplicaSetsByOwner = model.RelatedIndex{Kind: keyReplicaSets, Name: indexByOwner, Keys: controllerKeys}
	// JobsByOwner indexes jobs by their controlling owner, as "<namespace>/<owner UID>": a Cron
	// Job's jobs.
	JobsByOwner = model.RelatedIndex{Kind: keyJobs, Name: indexByOwner, Keys: controllerKeys}
)

// OwnerKey is the key PodsByOwner, ReplicaSetsByOwner and JobsByOwner find the objects an owner
// controls under.
func OwnerKey(namespace, uid string) string { return namespace + "/" + uid }

// controllerKeys files an object under its controlling owner: the owner reference marked as the
// controller (a pod has at most one), in the object's namespace. An object without one, or whose
// controller reference names no UID, is under no key.
func controllerKeys(obj *unstructured.Unstructured) []string {
	for _, o := range model.NestedSlice(obj, "metadata", "ownerReferences") {
		if isController, _ := model.NestedBool(o, "controller"); !isController {
			continue
		}
		if uid := model.NestedString(o, "uid"); uid != "" {
			return []string{OwnerKey(model.NestedString(obj, "metadata", "namespace"), uid)}
		}
		return nil
	}
	return nil
}

// ownedPods returns the pods an object controls: through its controlled objects of via (a
// Deployment's replica sets, a Cron Job's jobs), or directly with no via.
func ownedPods(obj *unstructured.Unstructured, rel model.Related, via *model.RelatedIndex) []*unstructured.Unstructured {
	ns := model.NestedString(obj, "metadata", "namespace")
	owners := []string{model.NestedString(obj, "metadata", "uid")}
	if via != nil {
		owners = owners[:0]
		for _, o := range rel.Objects(*via, OwnerKey(ns, model.NestedString(obj, "metadata", "uid"))) {
			owners = append(owners, model.NestedString(o, "metadata", "uid"))
		}
	}
	var pods []*unstructured.Unstructured
	for _, uid := range owners {
		if uid == "" {
			continue
		}
		pods = append(pods, rel.Objects(PodsByOwner, OwnerKey(ns, uid))...)
	}
	return pods
}

// withPods returns detail as a DetailWith that appends the Pods table of the pods the object
// controls (through via, if set).
func withPods(detail func(*unstructured.Unstructured) []model.DetailSection, via *model.RelatedIndex) func(*unstructured.Unstructured, model.Related) []model.DetailSection {
	return func(obj *unstructured.Unstructured, rel model.Related) []model.DetailSection {
		sections := detail(obj)
		if uid := model.NestedString(obj, "metadata", "uid"); uid == "" {
			return sections
		}
		if s, ok := podsSection(ownedPods(obj, rel, via), true); ok {
			sections = append(sections, s)
		}
		return sections
	}
}

// The workloads' DetailNeeds: the indexes their Pods table reads.
var (
	directPodsNeeds     = []model.RelatedIndex{PodsByOwner}
	deploymentPodsNeeds = []model.RelatedIndex{ReplicaSetsByOwner, PodsByOwner}
	cronJobPodsNeeds    = []model.RelatedIndex{JobsByOwner, PodsByOwner}
)
