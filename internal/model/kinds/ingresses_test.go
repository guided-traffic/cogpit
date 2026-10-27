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
			sections := p.DetailSections(c.obj, model.NoRelated)
			for _, l := range c.want {
				if it := findItem(sections, l.Text); it.NameURL != l.URL || it.Value == "" {
					t.Errorf("rule %s: name link %q, want %q", l.Text, it.NameURL, l.URL)
				}
			}
		})
	}
}
