package kinds

import (
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/guided-traffic/cogpit/internal/model"
)

// The namespace controller's conditions on a namespace being deleted (Kubernetes'
// pkg/controller/namespace/deletion, read 2026-10-19 and seen on kind v1.36.1): each is False
// while all is well, and True says what holds the deletion.
const (
	CondContentRemaining    = "NamespaceContentRemaining"
	CondFinalizersRemaining = "NamespaceFinalizersRemaining"
	CondDiscoveryFailure    = "NamespaceDeletionDiscoveryFailure"
	CondParsingFailure      = "NamespaceDeletionGroupVersionParsingFailure"
	CondContentFailure      = "NamespaceDeletionContentFailure"
)

// maxRemainingEntries bounds the entries read from one condition's message.
const maxRemainingEntries = 200

// NamespaceDeletion is what a namespace's own object says about its deletion: whether it is being
// deleted, and what the namespace controller reported holding it. The controller names the kinds
// that still have objects in the namespace by resource and group, and the finalizers those objects
// hold, each with a count, but never an object's name, and it does not pair a kind with its
// finalizers. Anyone who may read the namespace reads it (checked on kind, 2026-10-19, WF43).
type NamespaceDeletion struct {
	// Terminating is true while the namespace is being deleted (a deletionTimestamp, or the phase).
	Terminating bool
	// Reported is true once the controller has written any of its conditions.
	Reported bool
	// Remaining are the kinds NamespaceContentRemaining names, in its order (the controller sorts
	// them); nil while it is False or absent.
	Remaining []RemainingResource
	// Finalizers are the finalizers NamespaceFinalizersRemaining names, in its order.
	Finalizers []RemainingFinalizer
	// DiscoveryFailure is NamespaceDeletionDiscoveryFailure's message while it is True: the
	// controller could not list every kind the server serves (an aggregated API that does not
	// answer), so it cannot delete their objects and keeps the namespace.
	DiscoveryFailure string
	// Failures are the controller's other conditions that are True (a group version it could not
	// parse, content it failed to delete), by type, with their messages.
	Failures []NamespaceFailure
}

// RemainingResource is one kind the namespace still holds objects of.
type RemainingResource struct {
	schema.GroupResource
	// Count is how many objects of the kind the controller found at its last pass.
	Count int64
}

// String names the resource as the controller does, "configmaps" or "widgets.example.com".
func (r RemainingResource) String() string {
	if r.Group == "" {
		return r.Resource
	}
	return r.Resource + "." + r.Group
}

// RemainingFinalizer is one finalizer objects in the namespace still hold, and on how many.
type RemainingFinalizer struct {
	Name  string
	Count int64
}

// NamespaceFailure is one of the controller's failure conditions that is True.
type NamespaceFailure struct {
	Type, Message string
}

// Blocked reports whether the controller reported anything that holds the deletion.
func (d NamespaceDeletion) Blocked() bool {
	return len(d.Remaining) > 0 || len(d.Finalizers) > 0 || d.DiscoveryFailure != "" || len(d.Failures) > 0
}

// NamespaceDeletionOf reads a namespace's deletion from its object. Any shape is tolerated: what
// does not parse is left out.
func NamespaceDeletionOf(obj *unstructured.Unstructured) NamespaceDeletion {
	var d NamespaceDeletion
	if obj == nil {
		return d
	}
	_, token := namespaceStatus(obj)
	d.Terminating = token == model.StatusTerminating
	for _, c := range conditions(obj.Object, "status", "conditions") {
		switch c.Type {
		case CondContentRemaining, CondFinalizersRemaining, CondDiscoveryFailure, CondParsingFailure, CondContentFailure:
			d.Reported = true
		default:
			continue
		}
		if c.Status != condTrue {
			continue
		}
		switch c.Type {
		case CondContentRemaining:
			d.Remaining = parseRemainingResources(c.Message)
		case CondFinalizersRemaining:
			d.Finalizers = parseRemainingFinalizers(c.Message)
		case CondDiscoveryFailure:
			d.DiscoveryFailure = c.Message
			if d.DiscoveryFailure == "" {
				d.DiscoveryFailure = c.Reason
			}
		default:
			d.Failures = append(d.Failures, NamespaceFailure{Type: c.Type, Message: c.Message})
		}
	}
	return d
}

// parseRemainingResources reads "Some resources are remaining: configmaps. has 1 resource
// instances, widgets.example.com has 2 resource instances": the resource, then its group after the
// first dot (empty for the core group), and the count.
func parseRemainingResources(msg string) []RemainingResource {
	var out []RemainingResource
	for _, e := range remainingEntries(msg) {
		name, rest, ok := strings.Cut(e, " has ")
		if !ok {
			continue
		}
		resource, group, _ := strings.Cut(strings.TrimSpace(name), ".")
		if resource == "" || strings.ContainsAny(resource, " /") {
			continue
		}
		out = append(out, RemainingResource{GroupResource: schema.GroupResource{Group: group, Resource: resource}, Count: leadingCount(rest)})
	}
	return out
}

// parseRemainingFinalizers reads "Some content in the namespace has finalizers remaining:
// example.com/x in 2 resource instances, kubernetes.io/pvc-protection in 1 resource instances".
func parseRemainingFinalizers(msg string) []RemainingFinalizer {
	var out []RemainingFinalizer
	for _, e := range remainingEntries(msg) {
		i := strings.LastIndex(e, " in ")
		if i <= 0 {
			continue
		}
		name := strings.TrimSpace(e[:i])
		if name == "" || strings.Contains(name, " ") {
			continue
		}
		out = append(out, RemainingFinalizer{Name: name, Count: leadingCount(e[i+len(" in "):])})
	}
	return out
}

// remainingEntries splits a remaining condition's message after its "remaining: " into its
// comma-separated entries, at most maxRemainingEntries.
func remainingEntries(msg string) []string {
	_, list, ok := strings.Cut(msg, "remaining: ")
	if !ok {
		return nil
	}
	entries := strings.Split(list, ", ")
	if len(entries) > maxRemainingEntries {
		entries = entries[:maxRemainingEntries]
	}
	return entries
}

// leadingCount reads the number a text starts with ("2 resource instances"); 0 without one.
func leadingCount(s string) int64 {
	f := strings.Fields(s)
	if len(f) == 0 {
		return 0
	}
	n, err := strconv.ParseInt(f[0], 10, 64)
	if err != nil || n < 0 {
		return 0
	}
	return n
}
