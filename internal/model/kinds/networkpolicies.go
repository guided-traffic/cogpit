package kinds

import (
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/model"
)

const (
	policyIngress = "Ingress"
	policyEgress  = "Egress"
)

// networkPolicies has the columns of Lens's documentation (not captured): Name, Namespace, Policy
// Types, Age. Policy Types reads "Ingress, Egress" (inferred). A policy has no state, so no
// status token. The drawer is inferred.
var networkPolicies = &model.KindProfile{
	Key:        keyNetworkPolicies,
	Group:      groupNetworking,
	Version:    "v1",
	Resource:   "networkpolicies",
	Kind:       "NetworkPolicy",
	Title:      "Network Policies",
	Namespaced: true,
	Columns: []model.Column{
		nameColumn,
		namespaceColumn,
		{ID: "policyTypes", Title: "Policy Types", Type: model.ColumnText},
		ageColumn,
	},
	Cells: networkPolicyCells,
	Fields: map[string]fieldFunc{
		"policyType": func(obj *unstructured.Unstructured) []string {
			return policyTypes(obj)
		},
		"podSelector": func(obj *unstructured.Unstructured) []string {
			return labelSelector(obj, "spec", "podSelector")
		},
	},
	Detail: networkPolicyDetail,
}

func networkPolicyCells(obj *unstructured.Unstructured) ([]model.Cell, model.Status) {
	return []model.Cell{
		nameCell(obj),
		namespaceCell(obj),
		{Text: strings.Join(policyTypes(obj), ", ")},
		model.AgeCell(created(obj)),
	}, model.StatusNone
}

// policyTypes is spec.policyTypes. The API server defaults an empty list to Ingress, plus Egress
// when the policy has egress rules, so an object built without the default reads the same.
func policyTypes(obj any) []string {
	if types := model.NestedStrings(obj, "spec", "policyTypes"); len(types) > 0 {
		return types
	}
	types := []string{policyIngress}
	if len(model.NestedSlice(obj, "spec", "egress")) > 0 {
		types = append(types, policyEgress)
	}
	return types
}

// networkPolicyDetail is Cogpit's drawer for a policy (inferred, Lens's is not captured): the pods
// it selects and its types in Properties, then one section per direction it governs with a line
// per rule, saying what it allows. A direction without rules denies all traffic of the selected
// pods in that direction.
func networkPolicyDetail(obj *unstructured.Unstructured) []model.DetailSection {
	var d detailItems
	pods := strings.Join(labelSelector(obj, "spec", "podSelector"), "\n")
	if pods == "" {
		pods = "every pod in the namespace"
	}
	d.add("Pod Selector", pods)
	types := policyTypes(obj)
	d.add("Policy Types", strings.Join(types, ", "))
	sections := withSection(nil, model.TitleProperties, d)
	for _, dir := range []struct{ typ, rules, peers, peerTitle, direction string }{
		{policyIngress, "ingress", "from", "From", "incoming"},
		{policyEgress, "egress", "to", "To", "outgoing"},
	} {
		if !contains(types, dir.typ) {
			continue
		}
		var items detailItems
		rules := model.NestedSlice(obj, "spec", dir.rules)
		if len(rules) == 0 {
			items.addStatus("Rules", "none: all "+dir.direction+" traffic of the selected pods is denied", model.StatusWarning)
		}
		for i, r := range rules {
			items.add("Rule "+itoa(int64(i+1)), dir.peerTitle+": "+policyPeers(model.NestedSlice(r, dir.peers))+
				"\nPorts: "+policyPorts(model.NestedSlice(r, "ports")))
		}
		sections = withSection(sections, dir.typ, items)
	}
	return sections
}

// policyPeers renders a rule's peers, "; "-separated: "pods app=web in namespaces team=a",
// "10.0.0.0/8 except 10.1.0.0/16"; "anywhere" for none, which allows every peer.
func policyPeers(peers []any) string {
	if len(peers) == 0 {
		return "anywhere"
	}
	out := make([]string, 0, len(peers))
	for _, p := range peers {
		if cidr := model.NestedString(p, "ipBlock", "cidr"); cidr != "" {
			if except := model.NestedStrings(p, "ipBlock", "except"); len(except) > 0 {
				cidr += " except " + strings.Join(except, ", ")
			}
			out = append(out, cidr)
			continue
		}
		// A selector that is set but empty selects everything; a namespace selector that is not
		// set means the policy's own namespace.
		s := "all pods"
		if pods := selectorText(p, "podSelector"); pods != "" {
			s = "pods " + pods
		}
		_, hasNS := model.NestedValue(p, "namespaceSelector")
		nss := selectorText(p, "namespaceSelector")
		switch {
		case nss != "":
			s += " in namespaces " + nss
		case hasNS:
			s += " in all namespaces"
		default:
			s += " in this namespace"
		}
		out = append(out, s)
	}
	return strings.Join(out, "; ")
}

// selectorText renders the label selector at field, "" when it is not set or selects everything.
func selectorText(root any, field string) string {
	return strings.Join(labelSelector(root, field), ", ")
}

// policyPorts renders a rule's ports: "80/TCP, 32000-32768/TCP, http/TCP"; "all" for none.
func policyPorts(ports []any) string {
	if len(ports) == 0 {
		return "all"
	}
	out := make([]string, 0, len(ports))
	for _, p := range ports {
		port := model.NestedIntOrString(p, "port")
		if port == "" {
			port = "all"
		} else if end := model.NestedIntOrString(p, "endPort"); end != "" {
			port += "-" + end
		}
		out = append(out, port+"/"+protocol(p))
	}
	return strings.Join(out, ", ")
}
