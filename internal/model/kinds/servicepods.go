package kinds

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/model"
)

// A Service's Selected Pods in its drawer (owner, D-165): the pods its selector matches, each a link to the
// pod's drawer, with Ready from its endpoint slices' ready condition, the endpoints kube-proxy
// routes to (Ready green, Not ready red, as tinted text). The section stands before Endpoint
// Slices. The pods come from the namespace's pods (PodsByNamespace, a DetailNeeds: only an open
// drawer holds them), matched against spec.selector as Kubernetes's endpoint controller matches.
// A Service without a selector, or an ExternalName Service, has a note in place of the table: no
// pods are chosen for it.

const (
	titlePodsSection = "Selected Pods"
	textNotReady     = "Not ready"
)

// servicePodColumns are the Selected Pods section's columns.
var servicePodColumns = []model.Column{
	nameColumn,
	{ID: fieldReady, Title: nodeReady, Type: model.ColumnText},
}

// servicePodsSection returns the Service's Selected Pods section; false while its namespace's pods are
// still listing.
func servicePodsSection(obj *unstructured.Unstructured, rel model.Related) (model.DetailSection, bool) {
	note := func(text string) (model.DetailSection, bool) {
		return model.DetailSection{Title: titlePodsSection, Items: []model.DetailItem{{Value: text}}}, true
	}
	if serviceType(obj) == serviceExternalName {
		return note("ExternalName: traffic goes to " + model.NestedString(obj, "spec", "externalName") + ", no pods.")
	}
	selector := model.NestedStringMap(obj, "spec", "selector")
	if len(selector) == 0 {
		return note("No selector: Kubernetes picks no pods for this Service; its endpoints are in Endpoint Slices.")
	}
	ns := obj.GetNamespace()
	var pods []*unstructured.Unstructured
	for _, p := range rel.Objects(PodsByNamespace, ns) {
		if selectorMatches(selector, p.GetLabels()) {
			pods = append(pods, p)
		}
	}
	if len(pods) == 0 {
		if !rel.Listed(PodsByNamespace) {
			return model.DetailSection{}, false
		}
		return note("No pods match the selector")
	}
	sortByName(pods)
	slices := rel.Objects(EndpointSlicesByService, ns+"/"+obj.GetName())
	endpoints := podEndpoints(slices)
	listed := rel.Listed(EndpointSlicesByService)
	rows := make([][]model.Cell, 0, len(pods))
	for _, p := range pods {
		rows = append(rows, []model.Cell{
			{Text: p.GetName(), Link: linkTo(podsResource, ns, p.GetName())},
			podTrafficCell(p, endpoints, listed),
		})
	}
	return model.DetailSection{Title: titlePodsSection, Table: model.CellTable(servicePodColumns, rows)}, true
}

// selectorMatches reports whether labels hold every pair of a Service's selector.
func selectorMatches(selector, labels map[string]string) bool {
	for k, v := range selector {
		if got, ok := labels[k]; !ok || got != v {
			return false
		}
	}
	return true
}

// podEndpoint is what a Service's endpoint slices say about one pod, over all its endpoints (one
// per address family): ready when any is, terminating when every one is.
type podEndpoint struct {
	ready, terminating bool
}

// podEndpoints collects the slices' endpoints that point at a pod, by the pod's UID, else (a
// targetRef without one) by "name:" and its name.
func podEndpoints(slices []*unstructured.Unstructured) map[string]podEndpoint {
	out := map[string]podEndpoint{}
	for _, s := range slices {
		for _, e := range model.NestedSlice(s, fieldEndpoints) {
			ref := model.NestedMap(e, "targetRef")
			if model.NestedString(ref, "kind") != kindPod {
				continue
			}
			key := model.NestedString(ref, "uid")
			if key == "" {
				key = "name:" + model.NestedString(ref, "name")
			}
			ready, ok := model.NestedBool(e, "conditions", fieldReady)
			ready = ready || !ok // unset reads as ready (discovery.k8s.io/v1)
			terminating, _ := model.NestedBool(e, "conditions", "terminating")
			pe, seen := out[key]
			if !seen {
				pe.terminating = true
			}
			pe.ready = pe.ready || ready
			pe.terminating = pe.terminating && terminating
			out[key] = pe
		}
	}
	return out
}

// podTrafficCell is a pod's Ready: Ready when an endpoint of the Service's slices for it is ready,
// else Not ready, with the reason when the slices hold no endpoint for it; Unknown while the
// slices are not known.
func podTrafficCell(pod *unstructured.Unstructured, endpoints map[string]podEndpoint, listed bool) model.Cell {
	notReady := func(reason string) model.Cell {
		text := textNotReady
		if reason != "" {
			text += " (" + reason + ")"
		}
		return model.Cell{Text: text, Status: model.StatusError, Tint: true}
	}
	pe, ok := endpoints[string(pod.GetUID())]
	if !ok {
		pe, ok = endpoints["name:"+pod.GetName()]
	}
	switch {
	case ok && pe.ready:
		return model.Cell{Text: nodeReady, Status: model.StatusOK, Tint: true}
	case ok && pe.terminating:
		return notReady("terminating")
	case ok:
		return notReady("")
	case !listed:
		return model.Cell{Text: "Unknown", Status: model.StatusUnknown, Tint: true}
	case pod.GetDeletionTimestamp() != nil:
		return notReady("terminating")
	}
	switch phase := model.NestedString(pod, "status", "phase"); {
	case phase == podSucceeded || phase == podFailed:
		return notReady(phase)
	case model.NestedString(pod, "status", "podIP") == "":
		return notReady("no IP yet")
	}
	return notReady("in no endpoint slice")
}
