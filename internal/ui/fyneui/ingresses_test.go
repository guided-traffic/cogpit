package fyneui

import (
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
)

// The Ingresses view and an Ingress's drawer. Every host is made up. PNGs:
// testdata/{ingresses,ingress_drawer}_dark.png.

// fixtureIngressObjects: hosts with and without TLS, two rules and two paths, a wildcard host and a
// rule without a host, a host wider than the drawer's name column.
func fixtureIngressObjects() []*unstructured.Unstructured {
	rule := func(host string, paths ...string) any {
		ps := make([]any, 0, len(paths))
		for _, p := range paths {
			ps = append(ps, map[string]any{"path": p, "pathType": "Prefix",
				"backend": map[string]any{"service": map[string]any{"name": "web", "port": map[string]any{"number": int64(80)}}}})
		}
		r := map[string]any{"http": map[string]any{"paths": ps}}
		if host != "" {
			r["host"] = host
		}
		return r
	}
	ingress := func(ns, name string, tls []any, rules ...any) *unstructured.Unstructured {
		spec := map[string]any{"ingressClassName": "nginx", "rules": rules}
		if tls != nil {
			spec["tls"] = tls
		}
		return &unstructured.Unstructured{Object: map[string]any{"apiVersion": "networking.k8s.io/v1", "kind": "Ingress",
			"metadata": map[string]any{"name": name, "namespace": ns, "uid": "uid-ing-" + name, "creationTimestamp": ts(40 * 24 * time.Hour)},
			"spec":     spec,
			"status":   map[string]any{"loadBalancer": map[string]any{"ingress": []any{map[string]any{"ip": "203.0.113.30"}}}}}}
	}
	tls := func(hosts ...any) []any { return []any{map[string]any{"hosts": hosts, "secretName": "web-tls"}} }
	return []*unstructured.Unstructured{
		ingress("ops", "admin", nil, rule("admin.example.internal", "/")),
		ingress("git", "gitlab-kas", tls("kas.example.net"), rule("kas.example.net", "/k8s-proxy/", "/"), rule("kas-internal.example.net", "/")),
		ingress("pad", "notes", tls("pad.example.de"), rule("pad.example.de", "/", "/notes_websocket")),
		ingress("monitoring", "status-board", tls("status-board.monitoring.k8s-main.example.net"),
			rule("status-board.monitoring.k8s-main.example.net", "/")),
		ingress("shop", "storefront", tls("shop.example.com", "www.shop.example.com"),
			rule("shop.example.com", "/api", "/"), rule("www.shop.example.com", "/")),
		ingress("saas", "tenants", nil, rule("*.tenants.example.com", "/"), rule("", "/healthz"), rule("parked.example.com", "/")),
	}
}

// ingressFake is a connected cluster that serves Ingresses, the fixture's in its list and drawers.
func ingressFake(t testing.TB) *fakeEngine {
	f := connectedFake(t, 1)
	f.kinds = append(f.kinds, kindInfo{Kind: "ingresses", Title: "Ingresses", APIKind: "Ingress", Group: "networking.k8s.io", Version: "v1", Namespaced: true})
	objs := fixtureIngressObjects()
	f.tables["ingresses"] = kindSnapshot(t, "ingresses", objs)
	p, _ := kinds.ByKey("ingresses")
	for _, o := range objs {
		ref := model.ObjectRef{Context: "demo", Kind: "ingresses", Namespace: o.GetNamespace(), Name: o.GetName()}
		m, err := engine.BuildDetailWith(p, ref, o, nil, fixedNow, model.NoRelated)
		if err != nil {
			t.Fatal(err)
		}
		f.details[ref.String()] = m
	}
	f.ui.View = "ingresses"
	return f
}

// TestRenderIngressesDark: the Ingresses view, the pointer on storefront's Rules.
func TestRenderIngressesDark(t *testing.T) {
	f := ingressFake(t)
	sh, w, q := settledShell(t, f, "ingresses")
	sh.tip.delay = 0
	c := cellAt(t, sh, rowOf(t, f, "ingresses", "storefront"), columnOf(t, sh, "rules"))
	test.MoveMouse(w.Canvas(), fyne.CurrentApp().Driver().AbsolutePositionForObject(c).AddXY(c.Size().Width/2, c.Size().Height/2))
	q.until(t, "the tooltip", func() bool { return sh.tip.visibleText() != "" })
	savePNG(t, w, "ingresses_dark.png")
}

// TestRenderIngressDrawerDark: gitlab-kas's drawer: two rules, one of them with two paths.
func TestRenderIngressDrawerDark(t *testing.T) {
	f := ingressFake(t)
	ref := model.ObjectRef{Context: "demo", Kind: "ingresses", Namespace: "git", Name: "gitlab-kas"}
	f.ui.Selected, f.ui.Drawer = []model.ObjectRef{ref}, &ref
	sh, w, q := newTestShell(t, f, Options{})
	q.until(t, "the drawer", func() bool { return sh.drawer.last != nil && len(sh.drawer.details.Objects) > 2 && sh.diag.total > 0 })
	savePNG(t, w, "ingress_drawer_dark.png")
}
