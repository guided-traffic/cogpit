package kinds

import (
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/model"
)

// Service types the profile treats specially.
const (
	serviceClusterIP    = "ClusterIP"
	serviceLoadBalancer = "LoadBalancer"
	serviceExternalName = "ExternalName"
)

var services = &model.KindProfile{
	Key:        "services",
	Version:    "v1",
	Resource:   "services",
	Kind:       "Service",
	Title:      "Services",
	Namespaced: true,
	Columns: []model.Column{
		nameColumn,
		namespaceColumn,
		typeColumn,
		{ID: "clusterIP", Title: "Cluster IP", Type: model.ColumnText},
		{ID: "ports", Title: "Ports", Type: model.ColumnText},
		{ID: idExternalIP, Title: "External IP", Type: model.ColumnText},
		{ID: idSelector, Title: "Selector", Type: model.ColumnText},
		ageColumn,
		statusColumn,
	},
	Cells: serviceCells,
	Fields: map[string]fieldFunc{
		"clusterIP": func(obj *unstructured.Unstructured) []string {
			return clusterIPs(obj)
		},
		idExternalIP: func(obj *unstructured.Unstructured) []string {
			return serviceExternal(obj)
		},
		idType: func(obj *unstructured.Unstructured) []string {
			return []string{serviceType(obj)}
		},
		"port": servicePortNumbers,
		idSelector: func(obj *unstructured.Unstructured) []string {
			return model.SortedPairs(model.NestedStringMap(obj, "spec", "selector"))
		},
	},
	Detail:      serviceDetail,
	Needs:       []model.RelatedIndex{EndpointSlicesByService},
	DetailNeeds: []model.RelatedIndex{ForwardsByTarget}, // the ports' forwards, only the open drawer reads them
	DetailWith:  serviceDetailWith,                      // in endpointslices.go
}

func serviceCells(obj *unstructured.Unstructured) ([]model.Cell, model.Status) {
	typ := serviceType(obj)
	text, token := serviceStatus(obj, typ)
	return []model.Cell{
		nameCell(obj),
		namespaceCell(obj),
		{Text: typ},
		{Text: model.NestedString(obj, "spec", "clusterIP")},
		{Text: strings.Join(servicePorts(obj), ", ")},
		{Text: orDash(serviceExternal(obj))},
		{Text: strings.Join(model.SortedPairs(model.NestedStringMap(obj, "spec", "selector")), ", ")},
		model.AgeCell(created(obj)),
		{Text: text, Status: token},
	}, token
}

// serviceType returns spec.type, which the API defaults to ClusterIP.
func serviceType(obj any) string {
	if t := model.NestedString(obj, "spec", "type"); t != "" {
		return t
	}
	return serviceClusterIP
}

// serviceStatus is Active, or Pending for a load balancer that has no ingress yet.
func serviceStatus(obj any, typ string) (string, model.Status) {
	if typ == serviceLoadBalancer && len(loadBalancerIngress(obj)) == 0 {
		return "Pending", model.StatusPending
	}
	return "Active", model.StatusOK
}

// servicePorts renders each port as "80/TCP", or "443:30443/TCP" when it has a node port.
func servicePorts(obj any) []string {
	ports := model.NestedSlice(obj, "spec", "ports")
	out := make([]string, 0, len(ports))
	for _, p := range ports {
		port := model.NestedIntOrString(p, "port")
		if port == "" {
			continue
		}
		if node := model.NestedIntOrString(p, "nodePort"); node != "" && node != "0" {
			port += ":" + node
		}
		out = append(out, port+"/"+protocol(p))
	}
	return out
}

// servicePortNumbers is the "port" field: every port, node port and target port, so that an
// agent finds a service by any number a client or a pod uses.
func servicePortNumbers(obj *unstructured.Unstructured) []string {
	var out []string
	for _, p := range model.NestedSlice(obj, "spec", "ports") {
		node := model.NestedIntOrString(p, "nodePort")
		if node == "0" {
			node = ""
		}
		out = appendUnique(out, model.NestedIntOrString(p, "port"), node, model.NestedIntOrString(p, "targetPort"))
	}
	return out
}

// loadBalancerIngress returns the ingress points of a load balancer: IPs, else hostnames.
func loadBalancerIngress(obj any) []string {
	var out []string
	for _, in := range model.NestedSlice(obj, "status", "loadBalancer", "ingress") {
		addr := model.NestedString(in, "ip")
		if addr == "" {
			addr = model.NestedString(in, "hostname")
		}
		out = appendUnique(out, addr)
	}
	return out
}

// serviceExternal returns the addresses a service is reachable at from outside the cluster:
// load-balancer ingress, spec.externalIPs and, for an ExternalName service, the name it aliases.
func serviceExternal(obj any) []string {
	out := loadBalancerIngress(obj)
	out = appendUnique(out, model.NestedStrings(obj, "spec", "externalIPs")...)
	if serviceType(obj) == serviceExternalName {
		out = appendUnique(out, model.NestedString(obj, "spec", "externalName"))
	}
	return out
}

// clusterIPs returns spec.clusterIPs (dual-stack) and spec.clusterIP, which repeats the first.
// A headless service's "None" is kept: it is how one searches for headless services.
func clusterIPs(obj any) []string {
	out := appendUnique(nil, model.NestedStrings(obj, "spec", "clusterIPs")...)
	return appendUnique(out, model.NestedString(obj, "spec", "clusterIP"))
}

func serviceDetail(obj *unstructured.Unstructured) []model.DetailSection {
	return serviceDetailRel(obj, model.NoRelated)
}

// serviceDetailRel is serviceDetail with rel for the ports' forwards (model.NoRelated: none).
func serviceDetailRel(obj *unstructured.Unstructured, rel model.Related) []model.DetailSection {
	var d detailItems
	typ := serviceType(obj)
	d.add("Type", typ)
	d.add("Cluster IPs", strings.Join(clusterIPs(obj), ", "))
	d.add("IP Families", strings.Join(model.NestedStrings(obj, "spec", "ipFamilies"), ", "))
	d.add("External IPs", strings.Join(model.NestedStrings(obj, "spec", "externalIPs"), ", "))
	if typ == serviceExternalName {
		d.add("External Name", model.NestedString(obj, "spec", "externalName"))
	}
	if typ == serviceLoadBalancer {
		ingress := strings.Join(loadBalancerIngress(obj), ", ")
		if ingress == "" {
			ingress = "Pending"
		}
		d.add("Load Balancer Ingress", ingress)
	}
	for _, it := range servicePortItems(obj, listeningForwards(rel, obj.GetNamespace(), "Service", obj.GetName())) {
		d.put(it)
	}
	d.add("Selector", strings.Join(model.SortedPairs(model.NestedStringMap(obj, "spec", "selector")), "\n"))
	d.add("Session Affinity", model.NestedString(obj, "spec", "sessionAffinity"))
	d.add("External Traffic Policy", model.NestedString(obj, "spec", "externalTrafficPolicy"))
	return []model.DetailSection{{Title: "Connection", Items: d}}
}

// servicePortText renders a port as "name: 443/TCP -> 8443, node port 30443" (ASCII: Fyne's
// bundled font has no arrow glyph); "" for a port without a number.
func servicePortText(p any) string {
	port := model.NestedIntOrString(p, "port")
	if port == "" {
		return ""
	}
	line := port + "/" + protocol(p)
	if name := model.NestedString(p, "name"); name != "" {
		line = name + ": " + line
	}
	if target := model.NestedIntOrString(p, "targetPort"); target != "" {
		line += " -> " + target
	}
	if node := model.NestedIntOrString(p, "nodePort"); node != "" && node != "0" {
		line += ", node port " + node
	}
	return line
}
