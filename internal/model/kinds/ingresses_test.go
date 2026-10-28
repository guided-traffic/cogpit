package kinds_test

import (
	"slices"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
)

func FuzzIngresses(f *testing.F) { fuzzProfile(f, "ingresses") }

// Ingresses have no status token (inferred: Lens shows none, and an Ingress without an address is
// not necessarily broken); only a deletion makes the row terminating.
func TestIngressTexts(t *testing.T) {
	checkStatusTexts(t, []statusText{
		{"ingresses", "healthy", "loadBalancers", "203.0.113.30", model.StatusNone, model.StatusNone},
		{"ingresses", "healthy", "hosts", "shop.example.com, www.shop.example.com", model.StatusNone, model.StatusNone},
		{"ingresses", "failing", "loadBalancers", "-", model.StatusNone, model.StatusNone},
		{"ingresses", "failing", "hosts", "admin.example.internal", model.StatusNone, model.StatusNone},
		{"ingresses", "terminating", "loadBalancers", "legacy-lb.example.net", model.StatusNone, model.StatusTerminating},
		{"ingresses", "minimal", "hosts", "", model.StatusNone, model.StatusNone},
		{"ingresses", "minimal", "loadBalancers", "-", model.StatusNone, model.StatusNone},
		{"ingresses", "default-backend", "hosts", "*", model.StatusNone, model.StatusNone},
		{"ingresses", "hostname-lb", "hosts", "*.tenants.example.com, *, parked.example.com", model.StatusNone, model.StatusNone},
		{"ingresses", "hostname-lb", "loadBalancers", "k8s-saas-tenants-0123456789.elb.example.net", model.StatusNone, model.StatusNone},
	})
}

// TestIngressColumns pins Lens's documented columns (not captured) in their order, Hosts in the
// place of Rules (D-137).
func TestIngressColumns(t *testing.T) {
	assertColumnOrder(t, "ingresses", []model.Column{
		{ID: "name", Title: "Name"},
		{ID: "namespace", Title: "Namespace"},
		{ID: "loadBalancers", Title: "Load Balancers"},
		{ID: "hosts", Title: "Hosts"},
		{ID: "age", Title: "Age"},
	})
}

// TestIngressHostLinks: each host of the rules once, a link to https when a TLS entry covers it
// (by name, by a wildcard over its first label, or without hosts), else to http; "*" and a
// wildcard host are no links. The drawer's rules carry the same links on their hosts.
func TestIngressHostLinks(t *testing.T) {
	p, _ := kinds.ByKey("ingresses")
	rule := func(host string) any {
		r := map[string]any{"http": map[string]any{"paths": []any{map[string]any{"path": "/", "pathType": "Prefix",
			"backend": map[string]any{"service": map[string]any{"name": "web", "port": map[string]any{"number": int64(80)}}}}}}}
		if host != "" {
			r["host"] = host
		}
		return r
	}
	ingress := func(tls []any, hosts ...string) *unstructured.Unstructured {
		rules := make([]any, 0, len(hosts))
		for _, h := range hosts {
			rules = append(rules, rule(h))
		}
		spec := map[string]any{"rules": rules}
		if tls != nil {
			spec["tls"] = tls
		}
		return &unstructured.Unstructured{Object: map[string]any{"apiVersion": "networking.k8s.io/v1", "kind": "Ingress",
			"metadata": map[string]any{"name": "web", "namespace": "shop"}, "spec": spec}}
	}
	tlsFor := func(hosts ...any) []any { return []any{map[string]any{"hosts": hosts, "secretName": "web-tls"}} }
	cases := []struct {
		name string
		obj  *unstructured.Unstructured
		want []model.WebLink
	}{
		{"plain", ingress(nil, "shop.example.com"), []model.WebLink{{Text: "shop.example.com", URL: "http://shop.example.com"}}},
		{"tls by name", ingress(tlsFor("shop.example.com"), "shop.example.com", "admin.example.com"), []model.WebLink{
			{Text: "shop.example.com", URL: "https://shop.example.com"}, {Text: "admin.example.com", URL: "http://admin.example.com"}}},
		{"tls wildcard", ingress(tlsFor("*.example.com"), "shop.example.com", "a.shop.example.com", "example.com"), []model.WebLink{
			{Text: "shop.example.com", URL: "https://shop.example.com"},
			{Text: "a.shop.example.com", URL: "http://a.shop.example.com"},
			{Text: "example.com", URL: "http://example.com"}}},
		{"tls without hosts", ingress([]any{map[string]any{"secretName": "default"}}, "shop.example.com"),
			[]model.WebLink{{Text: "shop.example.com", URL: "https://shop.example.com"}}},
		{"once each", ingress(nil, "shop.example.com", "shop.example.com", "", ""), []model.WebLink{
			{Text: "shop.example.com", URL: "http://shop.example.com"}, {Text: "*"}}},
		{"wildcard host", ingress(tlsFor("*.example.com"), "*.example.com"), []model.WebLink{{Text: "*.example.com"}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cells, _ := p.Cells(c.obj)
			cell := cells[p.ColumnIndex("hosts")]
			if cell.Web == nil || !slices.Equal(*cell.Web, c.want) {
				t.Fatalf("hosts cell %+v, want %+v", cell.Web, c.want)
			}
			rules := findTable(t, p.DetailSections(c.obj, model.NoRelated), kinds.TitleIngressRules)
			for _, l := range c.want {
				i := slices.IndexFunc(rules.Rows, func(r []string) bool { return r[0] == l.Text })
				if i < 0 {
					t.Fatalf("no rule for %s in %q", l.Text, rules.Rows)
				}
				if got := webURL(rules.Cells[i][0]); got != l.URL {
					t.Errorf("rule %s: host link %q, want %q", l.Text, got, l.URL)
				}
			}
		})
	}
}

// webURL is the address of a cell of one web address, "" for none.
func webURL(c model.Cell) string {
	if c.Web == nil || len(*c.Web) != 1 {
		return ""
	}
	return (*c.Web)[0].URL
}

// TestIngressRulesTable: the drawer's Rules are a row per path, the host repeated. An Exact or
// Prefix path links to the host's address with the path; an ImplementationSpecific one does unless
// it holds a character of a regular expression, and no path of a wildcard host does. A Service
// backend links to the Service, a resource backend to its object when it is a built-in kind. A
// rule without paths goes to the Ingress's default backend, else to the controller's.
func TestIngressRulesTable(t *testing.T) {
	p, _ := kinds.ByKey("ingresses")
	svc := func(name string, port any) map[string]any {
		s := map[string]any{"name": name}
		switch port := port.(type) {
		case int64:
			s["port"] = map[string]any{"number": port}
		case string:
			s["port"] = map[string]any{"name": port}
		}
		return map[string]any{"service": s}
	}
	res := func(group, kind, name string) map[string]any {
		r := map[string]any{"kind": kind, "name": name}
		if group != "" {
			r["apiGroup"] = group
		}
		return map[string]any{"resource": r}
	}
	path := func(p, typ string, backend map[string]any) any {
		m := map[string]any{"pathType": typ, "backend": backend}
		if p != "" {
			m["path"] = p
		}
		return m
	}
	rule := func(host string, paths ...any) any {
		r := map[string]any{"host": host}
		if len(paths) > 0 {
			r["http"] = map[string]any{"paths": paths}
		}
		return r
	}
	ingress := func(spec map[string]any) *unstructured.Unstructured {
		return &unstructured.Unstructured{Object: map[string]any{"apiVersion": "networking.k8s.io/v1", "kind": "Ingress",
			"metadata": map[string]any{"name": "web", "namespace": "shop"}, "spec": spec}}
	}
	obj := ingress(map[string]any{
		"tls":            []any{map[string]any{"hosts": []any{"shop.example.com"}, "secretName": "shop-tls"}},
		"defaultBackend": svc("web", "http"),
		"rules": []any{
			rule("shop.example.com",
				path("/api", "Prefix", svc("api", int64(8080))),
				path("/v1/", "Exact", svc("api", "http")),
				path("/(.*)", "ImplementationSpecific", svc("web", int64(80))),
				path("", "ImplementationSpecific", svc("web", int64(80))),
				path("/static", "ImplementationSpecific", res("storage.example.com", "Bucket", "static")),
				path("/legacy", "Prefix", res("", "Service", "legacy"))),
			rule("*.tenants.example.com", path("/x", "Prefix", svc("web", int64(80)))),
			rule("admin.example.com"),
		},
	})
	service := func(name string) *model.ObjectRef {
		return &model.ObjectRef{Kind: "services", Namespace: "shop", Name: name}
	}
	want := []struct {
		row      []string
		pathURL  string
		backend  *model.ObjectRef
		hostLink string
	}{
		{[]string{"shop.example.com", "/api", "Prefix", "api:8080"}, "https://shop.example.com/api", service("api"), "https://shop.example.com"},
		{[]string{"shop.example.com", "/v1/", "Exact", "api:http"}, "https://shop.example.com/v1/", service("api"), "https://shop.example.com"},
		{[]string{"shop.example.com", "/(.*)", "ImplementationSpecific", "web:80"}, "", service("web"), "https://shop.example.com"},
		{[]string{"shop.example.com", "(no path)", "ImplementationSpecific", "web:80"}, "", service("web"), "https://shop.example.com"},
		{[]string{"shop.example.com", "/static", "ImplementationSpecific", "Bucket/static (storage.example.com)"}, "https://shop.example.com/static", nil, "https://shop.example.com"},
		{[]string{"shop.example.com", "/legacy", "Prefix", "Service/legacy"}, "https://shop.example.com/legacy", service("legacy"), "https://shop.example.com"},
		{[]string{"*.tenants.example.com", "/x", "Prefix", "web:80"}, "", service("web"), ""},
		{[]string{"admin.example.com", "(no paths)", "", "web:http"}, "", service("web"), "http://admin.example.com"},
	}
	rules := findTable(t, p.DetailSections(obj, model.NoRelated), kinds.TitleIngressRules)
	if len(rules.Rows) != len(want) {
		t.Fatalf("rows %q, want %d", rules.Rows, len(want))
	}
	for i, w := range want {
		if !slices.Equal(rules.Rows[i], w.row) {
			t.Errorf("row %d: %q, want %q", i, rules.Rows[i], w.row)
		}
		cells := rules.Cells[i]
		if got := webURL(cells[0]); got != w.hostLink {
			t.Errorf("row %d: host link %q, want %q", i, got, w.hostLink)
		}
		if got := webURL(cells[1]); got != w.pathURL {
			t.Errorf("row %d: path link %q, want %q", i, got, w.pathURL)
		}
		if got := cells[3].Link; (got == nil) != (w.backend == nil) || got != nil && *got != *w.backend {
			t.Errorf("row %d: backend link %+v, want %+v", i, got, w.backend)
		}
	}

	bare := ingress(map[string]any{"rules": []any{rule("admin.example.com")}})
	rules = findTable(t, p.DetailSections(bare, model.NoRelated), kinds.TitleIngressRules)
	if got := rules.Rows[0][3]; got != "(the controller's default backend)" || rules.Cells[0][3].Link != nil {
		t.Errorf("a rule without paths and without a default backend goes to %q (%+v)", got, rules.Cells[0][3].Link)
	}
}
