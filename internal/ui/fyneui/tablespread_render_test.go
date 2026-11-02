package fyneui

import (
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// The render of a table's free width (D-145): the Ingresses list in a window 2000 points wide,
// whose Hosts column, 340 points of its own, gets the width its host lists want and Name the rest.
// PNG: testdata/ingresses_spread_dark.png.

// acmeIngress is an Ingress of the acme shop with a rule per host (synthetic names only).
func acmeIngress(name, lb string, age time.Duration, hosts ...string) *unstructured.Unstructured {
	rules := make([]any, 0, len(hosts))
	for _, h := range hosts {
		rules = append(rules, map[string]any{"host": h, "http": map[string]any{"paths": []any{map[string]any{
			"path": "/", "pathType": "Prefix",
			"backend": map[string]any{"service": map[string]any{"name": name, "port": map[string]any{"number": int64(80)}}},
		}}}})
	}
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "networking.k8s.io/v1", "kind": "Ingress",
		"metadata": map[string]any{"name": name, "namespace": "acme-shop", "creationTimestamp": ts(age)},
		"spec":     map[string]any{"rules": rules},
		"status":   map[string]any{"loadBalancer": map[string]any{"ingress": []any{map[string]any{"ip": lb}}}},
	}}
}

func acmeIngresses() []*unstructured.Unstructured {
	return []*unstructured.Unstructured{
		acmeIngress("acme-api", "203.0.113.10", 26*time.Hour, "api.acme.example.net", "api-v2.acme.example.net"),
		acmeIngress("acme-checkout", "203.0.113.10", 3*time.Hour, "checkout.acme.example.net", "pay.acme.example.net", "plain.example.net"),
		acmeIngress("acme-docs", "203.0.113.11", 9*24*time.Hour, "docs.acme.example.net"),
		acmeIngress("acme-storefront", "203.0.113.11", 40*time.Minute, "shop.acme.example.net", "www.acme.example.net", "static.acme.example.net", "plain.example.net"),
	}
}

func TestRenderIngressesSpreadDark(t *testing.T) {
	f := connectedFake(t, 1)
	f.kinds = append(f.kinds, kindInfo{Kind: "ingresses", Title: "Ingresses", APIKind: "Ingress", Group: "networking.k8s.io", Version: "v1", Namespaced: true})
	f.tables["ingresses"] = kindSnapshot(t, "ingresses", acmeIngresses())
	f.ui.View = "ingresses"
	sh, w, q := newTestShell(t, f, Options{Theme: ThemeDark})
	w.Resize(fyne.NewSize(2000, 900))
	q.until(t, "the Ingresses", func() bool {
		return len(sh.kinds) > 0 && sh.view.snap != nil && sh.view.snap.View == "ingresses" && sh.diag.total > 0
	})
	settle(w)
	tv := sh.view
	if got, want := spreadWidthOf(t, tv, "hosts"), tv.contentWidth(tv.cols[3], len(tv.snap.Rows)); got != want {
		t.Fatalf("Hosts %v wide, want its content's %v", got, want)
	}
	savePNG(t, w, "ingresses_spread_dark.png")
}
