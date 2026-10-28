package kinds

import (
	"net/url"
	"slices"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/validation"

	"github.com/guided-traffic/cogpit/internal/model"
)

const (
	groupNetworking = "networking.k8s.io"
	// ingressClassAnnotation is the class annotation that predates spec.ingressClassName and that
	// controllers still honour.
	ingressClassAnnotation = "kubernetes.io/ingress.class"
	// anyHost stands for a rule without a host: it matches every host.
	anyHost = "*"
	// fieldHosts is the hosts of a TLS entry and the id of the columns that list hosts.
	fieldHosts = "hosts"
)

// ingresses has the columns of Lens's documentation (not captured), with Hosts in the place of
// Lens's Rules (the owner's choice, D-137): the rules' paths and backends are the drawer's. It has
// no status token: Lens shows none, and an Ingress without an address is not necessarily broken,
// because some controllers never publish one (inferred).
var ingresses = &model.KindProfile{
	Key:        "ingresses",
	Group:      groupNetworking,
	Version:    "v1",
	Resource:   "ingresses",
	Kind:       "Ingress",
	Title:      "Ingresses",
	Namespaced: true,
	Columns: []model.Column{
		nameColumn,
		namespaceColumn,
		{ID: "loadBalancers", Title: "Load Balancers", Type: model.ColumnText},
		// Room for two or three hosts before the cell cuts off; the full list is the tooltip.
		{ID: fieldHosts, Title: "Hosts", Type: model.ColumnText, Width: 340},
		ageColumn,
	},
	Cells: ingressCells,
	Fields: map[string]fieldFunc{
		"host": func(obj *unstructured.Unstructured) []string {
			return ingressHosts(obj)
		},
		fieldPath: func(obj *unstructured.Unstructured) []string {
			var out []string
			for _, r := range model.NestedSlice(obj, "spec", "rules") {
				for _, p := range model.NestedSlice(r, "http", "paths") {
					out = appendUnique(out, model.NestedString(p, fieldPath))
				}
			}
			return out
		},
		"ingressClass": func(obj *unstructured.Unstructured) []string {
			return nonEmpty(ingressClassName(obj))
		},
		fieldService: func(obj *unstructured.Unstructured) []string {
			return ingressServices(obj)
		},
		"address": func(obj *unstructured.Unstructured) []string {
			return ingressAddresses(obj)
		},
		"tlsSecret": func(obj *unstructured.Unstructured) []string {
			var out []string
			for _, t := range model.NestedSlice(obj, "spec", "tls") {
				out = appendUnique(out, model.NestedString(t, "secretName"))
			}
			return out
		},
	},
	Detail: ingressDetail,
}

func ingressCells(obj *unstructured.Unstructured) ([]model.Cell, model.Status) {
	return []model.Cell{
		nameCell(obj),
		namespaceCell(obj),
		{Text: orDash(loadBalancerIngress(obj))},
		model.WebCell(ingressHostLinks(obj)),
		model.AgeCell(created(obj)),
	}, model.StatusNone
}

// ingressHostLinks are the hosts the rules name, each once in the rules' order, as links that open
// the host in the browser: a rule without a host reads "*", and so does an Ingress without rules
// whose default backend answers every host. "*" and a wildcard host are no links.
func ingressHostLinks(obj any) []model.WebLink {
	var out []model.WebLink
	for _, r := range model.NestedSlice(obj, "spec", "rules") {
		host := ingressHost(r)
		if !slices.ContainsFunc(out, func(l model.WebLink) bool { return l.Text == host }) {
			out = append(out, model.WebLink{Text: host, URL: ingressHostURL(obj, host)})
		}
	}
	if len(out) == 0 && ingressBackend(model.NestedMap(obj, "spec", "defaultBackend")) != "" {
		out = append(out, model.WebLink{Text: anyHost})
	}
	return out
}

// ingressHostURL is the address a click on a rule's host opens: https when a TLS entry of the
// Ingress covers the host, else http, without a path; "" for what is no DNS name ("*", a
// wildcard host).
func ingressHostURL(obj any, host string) string {
	if u := ingressHostAddress(obj, host); u != nil {
		return u.String()
	}
	return ""
}

// ingressHostAddress is ingressHostURL as a URL, nil for what is no DNS name.
func ingressHostAddress(obj any, host string) *url.URL {
	if len(validation.IsDNS1123Subdomain(host)) > 0 {
		return nil
	}
	scheme := "http"
	if ingressTLSCovers(obj, host) {
		scheme = "https"
	}
	return &url.URL{Scheme: scheme, Host: host}
}

// ingressTLSCovers reports whether a TLS entry serves host: one that names it, one whose wildcard
// ("*.example.com") covers its first label, or one without hosts, which serves every host with the
// controller's default.
func ingressTLSCovers(obj any, host string) bool {
	_, parent, _ := strings.Cut(host, ".")
	for _, t := range model.NestedSlice(obj, "spec", "tls") {
		hosts := model.NestedStrings(t, fieldHosts)
		if len(hosts) == 0 {
			return true
		}
		for _, h := range hosts {
			if h == host || parent != "" && h == "*."+parent {
				return true
			}
		}
	}
	return false
}

func ingressHost(rule any) string {
	if h := model.NestedString(rule, "host"); h != "" {
		return h
	}
	return anyHost
}

// ingressBackend renders a backend as "service:port" (the port's number, else its name), or a
// resource backend as "Kind/name". It returns "" for a missing or empty backend.
func ingressBackend(b map[string]any) string {
	if svc := model.NestedMap(b, "service"); svc != nil {
		name := model.NestedString(svc, "name")
		port := model.NestedIntOrString(svc, "port", "number")
		if port == "" || port == "0" {
			port = model.NestedString(svc, "port", "name")
		}
		if name != "" && port != "" {
			return name + ":" + port
		}
		return name
	}
	if res := model.NestedMap(b, "resource"); res != nil {
		kind, name := model.NestedString(res, "kind"), model.NestedString(res, "name")
		if kind == "" || name == "" {
			return kind + name
		}
		return kind + "/" + name
	}
	return ""
}

// ingressClassName is spec.ingressClassName, else the older class annotation.
func ingressClassName(obj any) string {
	if c := model.NestedString(obj, "spec", "ingressClassName"); c != "" {
		return c
	}
	return model.NestedString(obj, "metadata", "annotations", ingressClassAnnotation)
}

// ingressHosts returns the hosts of the rules, then those of the TLS entries.
func ingressHosts(obj any) []string {
	var out []string
	for _, r := range model.NestedSlice(obj, "spec", "rules") {
		out = appendUnique(out, model.NestedString(r, "host"))
	}
	for _, t := range model.NestedSlice(obj, "spec", "tls") {
		out = appendUnique(out, model.NestedStrings(t, fieldHosts)...)
	}
	return out
}

// ingressServices returns the names of the backend services, the default backend's included.
func ingressServices(obj any) []string {
	out := appendUnique(nil, model.NestedString(obj, "spec", "defaultBackend", "service", "name"))
	for _, r := range model.NestedSlice(obj, "spec", "rules") {
		for _, p := range model.NestedSlice(r, "http", "paths") {
			out = appendUnique(out, model.NestedString(p, "backend", "service", "name"))
		}
	}
	return out
}

// ingressAddresses returns every IP and hostname of status.loadBalancer.ingress, for agents; the
// Load Balancers cell shows one address per entry, as the Services view does.
func ingressAddresses(obj any) []string {
	var out []string
	for _, in := range model.NestedSlice(obj, "status", "loadBalancer", "ingress") {
		out = appendUnique(out, model.NestedString(in, "ip"), model.NestedString(in, "hostname"))
	}
	return out
}

func ingressDetail(obj *unstructured.Unstructured) []model.DetailSection {
	var d detailItems
	// The class links to its IngressClass even when only the older annotation names it and no
	// such object exists: the drawer then says the object is missing.
	class := ingressClassName(obj)
	d.put(linkedItem("Class", class, linkTo(keyIngressClasses, "", class)))
	d.add("Load Balancers", strings.Join(ingressLoadBalancerLines(obj), "\n"))
	d.add("Default Backend", ingressBackendDetail(model.NestedMap(obj, "spec", "defaultBackend")))
	sections := withSection(nil, "Ingress", d)

	if t := ingressRulesTable(obj); t != nil {
		sections = append(sections, model.DetailSection{Title: TitleIngressRules, Table: t,
			Count: len(model.NestedSlice(obj, "spec", "rules"))})
	}

	// TLS entries name the Secret that holds the certificate, a link to it; Cogpit shows the
	// name, never the Secret's data. An entry without one takes the controller's default
	// certificate.
	entries := model.NestedSlice(obj, "spec", "tls")
	rows := make([][]model.Cell, 0, len(entries))
	for _, t := range entries {
		secret := model.Cell{Text: "(no secret)"}
		if name := model.NestedString(t, "secretName"); name != "" {
			secret = model.Cell{Text: name, Link: linkTo(keySecrets, obj.GetNamespace(), name)}
		}
		hosts := model.NestedStrings(t, fieldHosts)
		if len(hosts) == 0 {
			hosts = []string{anyHost}
		}
		rows = append(rows, []model.Cell{secret, {Text: strings.Join(hosts, "\n")}})
	}
	if len(rows) > 0 {
		sections = append(sections, model.DetailSection{Title: "TLS", Table: model.CellTable(ingressTLSColumns, rows)})
	}
	return sections
}

// ingressTLSColumns are the columns of the TLS section: a row per entry, its Secret and its hosts
// a line each.
var ingressTLSColumns = []model.Column{
	{ID: "tlsSecret", Title: titleSecret, Type: model.ColumnText},
	{ID: fieldHosts, Title: "Hosts", Type: model.ColumnText},
}

// TitleIngressRules is the title of an Ingress's Rules section, which the GUI draws in its own way.
const TitleIngressRules = "Rules"

// The path types whose path is a literal path; ImplementationSpecific leaves it to the controller.
const (
	pathTypeExact  = "Exact"
	pathTypePrefix = "Prefix"
)

// ingressRuleColumns are the columns of the Rules table: one row per path of each rule.
var ingressRuleColumns = []model.Column{
	{ID: "host", Title: "Host", Type: model.ColumnText},
	{ID: fieldPath, Title: "Path", Type: model.ColumnText},
	{ID: "pathType", Title: "Type", Type: model.ColumnText},
	{ID: "backend", Title: "Backend", Type: model.ColumnText},
}

// ingressRulesTable is the Rules section's table, nil without rules: a row per path, its rule's
// host repeated on each, the host and the path links to the browser (ingressHostURL,
// ingressPathURL) and the backend a link to its Service. A rule without paths is one row whose
// requests go to the Ingress's default backend, or, without one, to the controller's. The drawer
// draws each host once across its width with its paths under it (fyneui's ingressrules.go).
func ingressRulesTable(obj any) *model.DetailTable {
	ns := model.NestedString(obj, "metadata", "namespace")
	var rows [][]model.Cell
	for _, r := range model.NestedSlice(obj, "spec", "rules") {
		host := ingressHost(r)
		hostCell := webCell(host, ingressHostURL(obj, host))
		paths := model.NestedSlice(r, "http", "paths")
		if len(paths) == 0 {
			backend := ingressBackendCell(model.NestedMap(obj, "spec", "defaultBackend"), ns)
			if backend.Text == "" {
				backend.Text = "(the controller's default backend)"
			}
			rows = append(rows, []model.Cell{hostCell, {Text: "(no paths)"}, {}, backend})
			continue
		}
		for _, p := range paths {
			path, typ := model.NestedString(p, fieldPath), model.NestedString(p, "pathType")
			text := path
			if text == "" {
				text = "(no path)"
			}
			rows = append(rows, []model.Cell{hostCell, webCell(text, ingressPathURL(obj, host, path, typ)),
				{Text: typ}, ingressBackendCell(model.NestedMap(p, "backend"), ns)})
		}
	}
	if len(rows) == 0 {
		return nil
	}
	return model.CellTable(ingressRuleColumns, rows)
}

// webCell is a cell of one web address, plain text without one.
func webCell(text, url string) model.Cell {
	if url == "" {
		return model.Cell{Text: text}
	}
	return model.WebCell([]model.WebLink{{Text: text, URL: url}})
}

// ingressPathURL is the address a click on a rule's path opens: the host's (ingressHostURL) with
// the path. An Exact or Prefix path is a literal path; an ImplementationSpecific one may be the
// controller's regular expression (ingress-nginx's use-regex), so one with a character of a
// regular expression is no link. Nor is a path that does not start with "/", nor one of a host
// that is no link.
func ingressPathURL(obj any, host, path, typ string) string {
	u := ingressHostAddress(obj, host)
	if u == nil || !strings.HasPrefix(path, "/") {
		return ""
	}
	if typ != pathTypeExact && typ != pathTypePrefix && strings.ContainsAny(path, `()[]{}*+?^$|\`) {
		return ""
	}
	u.Path = path
	return u.String()
}

// ingressBackendCell is a backend's text (ingressBackendDetail) with a link to its Service, or to
// its resource when that is a built-in kind.
func ingressBackendCell(b map[string]any, ns string) model.Cell {
	c := model.Cell{Text: ingressBackendDetail(b)}
	if svc := model.NestedString(b, "service", "name"); svc != "" {
		c.Link = linkTo(keyServices, ns, svc)
	} else if res := model.NestedMap(b, "resource"); res != nil {
		apiVersion := "v1" // a resource backend without an API group is of the core group
		if g := model.NestedString(res, "apiGroup"); g != "" {
			apiVersion = g + "/"
		}
		c.Link = model.RefTo(apiVersion, model.NestedString(res, "kind"), ns, model.NestedString(res, "name"))
	}
	return c
}

// ingressBackendDetail is ingressBackend with a resource backend's API group: "Kind/name
// (group)".
func ingressBackendDetail(b map[string]any) string {
	s := ingressBackend(b)
	if g := model.NestedString(b, "resource", "apiGroup"); g != "" && s != "" {
		s += " (" + g + ")"
	}
	return s
}

// ingressLoadBalancerLines renders each load-balancer entry as its address with the ports the
// load balancer reports, and a port's error: "203.0.113.10 (80/TCP, 443/TCP: error text)".
func ingressLoadBalancerLines(obj any) []string {
	entries := model.NestedSlice(obj, "status", "loadBalancer", "ingress")
	out := make([]string, 0, len(entries))
	for _, in := range entries {
		addr := strings.Join(appendUnique(nil, model.NestedString(in, "ip"), model.NestedString(in, "hostname")), ", ")
		if addr == "" {
			continue
		}
		var ports []string
		for _, p := range model.NestedSlice(in, "ports") {
			port := model.NestedIntOrString(p, "port")
			if port == "" {
				continue
			}
			port += "/" + protocol(p)
			if e := model.NestedString(p, "error"); e != "" {
				port += ": " + singleLine(e)
			}
			ports = append(ports, port)
		}
		if len(ports) > 0 {
			addr += " (" + strings.Join(ports, ", ") + ")"
		}
		out = append(out, addr)
	}
	return out
}

// withSection appends a drawer section when it has items: a field the object does not set leaves
// no empty section behind.
func withSection(sections []model.DetailSection, title string, items detailItems) []model.DetailSection {
	if len(items) == 0 {
		return sections
	}
	return append(sections, model.DetailSection{Title: title, Items: items})
}
