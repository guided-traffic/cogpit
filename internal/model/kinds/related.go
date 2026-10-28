package kinds

import (
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/model"
)

// The related-objects indexes the built-in profiles declare in Needs (architecture §4.4).
var (
	// PodsByNode indexes pods by spec.nodeName: a node's pods.
	PodsByNode = model.RelatedIndex{Kind: podsResource, Name: "byNode", Keys: podNodeKeys}
	// PodsByClaim indexes pods by the persistent volume claims they mount, as
	// "<namespace>/<claimName>": a claim's pods.
	PodsByClaim = model.RelatedIndex{Kind: podsResource, Name: "byClaim", Keys: podClaimKeys}
	// PodsByPriorityClass indexes pods by spec.priorityClassName: a priority class's pods.
	PodsByPriorityClass = model.RelatedIndex{Kind: podsResource, Name: "byPriorityClass", Keys: podPriorityClassKeys}
	// ClusterRolesByName indexes cluster roles by name; Related.Listed tells a cluster role that
	// does not exist from one not listed yet (rbac.go).
	ClusterRolesByName = model.RelatedIndex{Kind: keyClusterRoles, Name: "byName", Keys: clusterRoleKeys}
	// EndpointSlicesByService indexes endpoint slices by the Service they belong to (their
	// kubernetes.io/service-name label), as "<namespace>/<service>": a Service's slices.
	EndpointSlicesByService = model.RelatedIndex{Kind: keyEndpointSlices, Name: "byService", Keys: sliceServiceKeys}
	// ForwardsByTarget indexes the port forwards by what they forward, as "<namespace>/Pod/<name>"
	// or "<namespace>/Service/<name>": the forwards a pod's or a service's ports show.
	ForwardsByTarget = model.RelatedIndex{Kind: keyPortForwards, Name: "byTarget", Keys: forwardTargetKeys}
	// CustomKindsByGroupKind indexes the custom resource definitions Cogpit shows a view for by
	// "<group>/<Kind>": a reference to a custom resource (an event's involved object) carries its
	// API version and Kind, and only the definition knows the plural its kind key needs.
	CustomKindsByGroupKind = model.RelatedIndex{Kind: KeyCustomResourceDefinitions, Name: "byGroupKind", Keys: crdGroupKindKeys}
)

// crdGroupKindKeys finds a definition under "<group>/<Kind>" once it is a view: established and
// serving a version (ParseCRD).
func crdGroupKindKeys(crd *unstructured.Unstructured) []string {
	k, err := ParseCRD(crd)
	if err != nil {
		return nil
	}
	return []string{k.Group + "/" + k.Kind}
}

// RefToWith is model.RefTo, which knows the built-in kinds, for the custom kinds too: a reference
// to a custom resource links to its kind's view when exactly one definition of rel's
// CustomKindsByGroupKind defines its group and Kind. Without the definitions (not listed yet, or
// not listable) it is nil, as RefTo's.
func RefToWith(rel model.Related, apiVersion, kind, namespace, name string) *model.ObjectRef {
	if ref := model.RefTo(apiVersion, kind, namespace, name); ref != nil || name == "" || rel == nil {
		return ref
	}
	group, _, found := strings.Cut(apiVersion, "/")
	if !found || group == "" {
		return nil
	}
	crds := rel.Objects(CustomKindsByGroupKind, group+"/"+kind)
	if len(crds) != 1 {
		return nil
	}
	k, err := ParseCRD(crds[0])
	if err != nil {
		return nil
	}
	if !k.Namespaced {
		namespace = ""
	}
	return &model.ObjectRef{Kind: k.Key(), Namespace: namespace, Name: name}
}

func sliceServiceKeys(slice *unstructured.Unstructured) []string {
	if svc := model.NestedString(slice, "metadata", "labels", serviceNameLabel); svc != "" {
		return []string{slice.GetNamespace() + "/" + svc}
	}
	return nil
}

func podNodeKeys(pod *unstructured.Unstructured) []string {
	if n := model.NestedString(pod, "spec", "nodeName"); n != "" {
		return []string{n}
	}
	return nil
}

func podClaimKeys(pod *unstructured.Unstructured) []string {
	var keys []string
	for _, v := range model.NestedSlice(pod, "spec", "volumes") {
		if c := model.NestedString(v, "persistentVolumeClaim", "claimName"); c != "" {
			keys = append(keys, pod.GetNamespace()+"/"+c)
		}
	}
	return keys
}

// ClaimKey is the key PodsByClaim finds a claim's pods under.
func ClaimKey(namespace, claim string) string { return namespace + "/" + claim }
