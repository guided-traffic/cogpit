package fyneui

import (
	"maps"
	"net/url"
	"slices"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/test"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
)

// The Ingresses view's Hosts column and an Ingress's drawer, whose hosts link to the browser
// (model.WebLink; D-137) and whose class and TLS Secrets link to their objects. Every host is made
// up. PNGs: testdata/{ingresses,ingress_drawer}_dark.png.

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
	f.kinds = append(f.kinds,
		kindInfo{Kind: "ingresses", Title: "Ingresses", APIKind: "Ingress", Group: "networking.k8s.io", Version: "v1", Namespaced: true},
		kindInfo{Kind: "ingressclasses", Title: "Ingress Classes", APIKind: "IngressClass", Group: "networking.k8s.io", Version: "v1"},
		kindInfo{Kind: "secrets", Title: "Secrets", APIKind: "Secret", Version: "v1", Namespaced: true})
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

// stubBrowser records the addresses the window opens in the browser instead of opening them.
func stubBrowser(t *testing.T) *[]string {
	var opened []string
	old := openBrowser
	openBrowser = func(_ fyne.App, u *url.URL) error { opened = append(opened, u.String()); return nil }
	t.Cleanup(func() { openBrowser = old })
	return &opened
}

// runAt is the middle of a cell's run, in the window.
func runAt(t *canvas.Text) fyne.Position {
	return fyne.CurrentApp().Driver().AbsolutePositionForObject(t).AddXY(t.Size().Width/2, t.Size().Height/2)
}

// cellRuns are a cell's runs as drawn.
func cellRuns(c *tableCell) []string {
	out := []string{c.text.Text}
	for _, m := range c.more {
		out = append(out, m.Text)
	}
	return out
}

// TestIngressHostsOpenInTheBrowser: the Hosts cell lists the hosts a comma apart; a click on one
// opens it in the browser (https where TLS covers it) and does not select the row; the pointer on
// a host underlines it alone, takes the hand and shows the address. A separator, a wildcard host
// and "*" are text: a click there selects the row.
func TestIngressHostsOpenInTheBrowser(t *testing.T) {
	opened := stubBrowser(t)
	f := ingressFake(t)
	sh, w, q := settledShell(t, f, "ingresses")
	hosts := columnOf(t, sh, "hosts")

	c := cellAt(t, sh, rowOf(t, f, "ingresses", "gitlab-kas"), hosts)
	if got := cellRuns(c); len(got) < 3 || got[0] != "kas.example.net" || got[1] != ", " || got[2] != "kas-internal.example.net" {
		t.Fatalf("hosts cell runs %q", got)
	}
	test.TapCanvas(w.Canvas(), runAt(c.text))
	test.TapCanvas(w.Canvas(), runAt(c.more[1]))
	if !slices.Equal(*opened, []string{"https://kas.example.net", "http://kas-internal.example.net"}) {
		t.Fatalf("opened %q", *opened)
	}

	test.MoveMouse(w.Canvas(), runAt(c.text))
	q.until(t, "the first host under the pointer", func() bool { return c.webHover == 0 })
	if !c.under.Visible() || c.under.Position().X != c.text.Position().X || c.under.Size().Width != c.text.Size().Width {
		t.Errorf("underline at %v, %v; the host at %v, %v", c.under.Position(), c.under.Size(), c.text.Position(), c.text.Size())
	}
	if c.Cursor() != desktop.PointerCursor || c.tipText() != "https://kas.example.net" {
		t.Errorf("cursor %v, tooltip %q", c.Cursor(), c.tipText())
	}
	if c.text.Color == c.more[1].Color {
		t.Error("the host under the pointer is drawn as the others")
	}
	test.MoveMouse(w.Canvas(), runAt(c.more[0]))
	q.until(t, "the separator under the pointer", func() bool { return c.webHover == -1 })
	if c.under.Visible() || c.Cursor() == desktop.PointerCursor {
		t.Error("the separator is drawn as a link")
	}

	tenants := cellAt(t, sh, rowOf(t, f, "ingresses", "tenants"), hosts)
	if tenants.full != "*.tenants.example.com, *, parked.example.com" {
		t.Fatalf("tenants' hosts %q", tenants.full)
	}
	test.TapCanvas(w.Canvas(), runAt(tenants.more[3]))
	if n := len(*opened); n != 3 || (*opened)[2] != "http://parked.example.com" {
		t.Fatalf("opened %q", *opened)
	}
	if n := len(f.callsOf("ui.select")); n != 0 {
		t.Errorf("a host's click selected the row (%d ui.select)", n)
	}
	test.TapCanvas(w.Canvas(), runAt(tenants.text)) // the wildcard host: text
	q.until(t, "ui.select", func() bool { return len(f.callsOf("ui.select")) == 1 })
	if len(*opened) != 3 {
		t.Errorf("a wildcard host opened %q", (*opened)[3:])
	}
}

// TestIngressRulesLinks: in the drawer each rule's host is a link on a line of its own, its paths
// under it: a path links to the host's address with the path, its address the tooltip, and the
// backend's Service name links to the Service while the port stays text. A host wider than the
// details wraps, and the pointer on any line underlines all.
func TestIngressRulesLinks(t *testing.T) {
	opened := stubBrowser(t)
	f := ingressFake(t)
	long := "a-very-long-subdomain-name-for-the-status-board-of-the-platform.monitoring-and-observability.platform-team.k8s-main.example.net"
	longRef := withIngressDetail(t, f, "long-host", nil, map[string]any{"host": long, "http": map[string]any{"paths": []any{
		ingressPath("/", "Prefix", "web", int64(80))}}})

	ref := model.ObjectRef{Context: "demo", Kind: "ingresses", Namespace: "git", Name: "gitlab-kas"}
	f.ui.Selected, f.ui.Drawer = []model.ObjectRef{ref}, &ref
	sh, w, q := newTestShell(t, f, Options{})
	q.until(t, "the drawer", func() bool { return sh.drawer.last != nil && len(sh.drawer.details.Objects) > 2 })
	settle(w)

	links := drawerLinks(sh)
	host, path, service := links["kas.example.net"], links["/k8s-proxy/"], links["web"]
	if host == nil || path == nil || service == nil {
		t.Fatalf("links %q, texts %q", slices.Collect(maps.Keys(links)), drawerTexts(sh))
	}
	if host.peers != nil {
		t.Error("a host that fits the details is wrapped")
	}
	if !slices.Contains(drawerTexts(sh), ":80") {
		t.Errorf("the port is not text after the Service's link: %q", drawerTexts(sh))
	}
	path.MouseIn(&desktop.MouseEvent{})
	q.until(t, "the path's tooltip", func() bool { return sh.tip.visibleText() == "https://kas.example.net/k8s-proxy/" })
	path.MouseOut()
	test.Tap(host)
	test.Tap(path)
	if !slices.Equal(*opened, []string{"https://kas.example.net", "https://kas.example.net/k8s-proxy/"}) {
		t.Fatalf("opened %q", *opened)
	}
	test.Tap(service)
	q.until(t, "ui.navigate", func() bool { return len(f.callsOf("ui.navigate")) == 1 })
	if got := decodeParams[refParams](t, f.callsOf("ui.navigate")[0]).Ref; got != (model.ObjectRef{Context: "demo", Kind: "services", Namespace: "git", Name: "web"}) {
		t.Errorf("backend link %+v", got)
	}

	sh.dispatch("ui.navigate", refParams{Ref: longRef}, nil)
	q.until(t, "the long host's drawer", func() bool { return drawerLinks(sh)["/"] != nil && sh.drawer.last.Ref == longRef })
	settle(w)
	var lines []*linkText
	for _, l := range findAll[*linkText](sh.drawer.details) {
		if l.peers != nil {
			lines = append(lines, l)
		}
	}
	if len(lines) < 2 {
		t.Fatalf("the long host is not wrapped into lines: %q", drawerTexts(sh))
	}
	lines[0].MouseIn(&desktop.MouseEvent{})
	if !lines[len(lines)-1].hovered {
		t.Error("the pointer on the first line does not underline the last")
	}
	lines[0].MouseOut()
	test.Tap(lines[len(lines)-1])
	if got := (*opened)[len(*opened)-1]; got != "http://"+long {
		t.Fatalf("opened %q", got)
	}
}

// TestIngressClassAndTLSSecretLinks: the drawer's Class links to the IngressClass and a TLS
// entry's Secret to the Secret in the Ingress's namespace; the hosts beside it stay text.
func TestIngressClassAndTLSSecretLinks(t *testing.T) {
	f := ingressFake(t)
	ref := withIngressDetail(t, f, "workspace", []any{"workspace.example.net"},
		map[string]any{"host": "workspace.example.net", "http": map[string]any{"paths": []any{ingressPath("/", "Prefix", "web", int64(80))}}})
	f.ui.Selected, f.ui.Drawer = []model.ObjectRef{ref}, &ref
	sh, _, q := newTestShell(t, f, Options{})
	q.until(t, "the drawer", func() bool { return drawerLinks(sh)["workspace-tls"] != nil })

	links := drawerLinks(sh)
	if links["nginx"] == nil || links["workspace.example.net"] == nil {
		t.Fatalf("links %q", slices.Collect(maps.Keys(links)))
	}
	if !slices.Contains(drawerTexts(sh), "workspace.example.net") {
		t.Errorf("the TLS entry's host is not text: %q", drawerTexts(sh))
	}
	test.Tap(links["nginx"])
	q.until(t, "the class's ui.navigate", func() bool { return len(f.callsOf("ui.navigate")) == 1 })
	test.Tap(links["workspace-tls"])
	q.until(t, "the Secret's ui.navigate", func() bool { return len(f.callsOf("ui.navigate")) == 2 })
	want := []model.ObjectRef{
		{Context: "demo", Kind: "ingressclasses", Name: "nginx"},
		{Context: "demo", Kind: "secrets", Namespace: "apps", Name: "workspace-tls"},
	}
	for i, c := range f.callsOf("ui.navigate") {
		if got := decodeParams[refParams](t, c).Ref; got != want[i] {
			t.Errorf("link %d went to %+v, want %+v", i, got, want[i])
		}
	}
}

// withIngressDetail gives the fake the drawer of an Ingress in namespace apps that is not in the
// list: name, its TLS hosts and its rules.
func withIngressDetail(t *testing.T, f *fakeEngine, name string, tlsHosts []any, rules ...any) model.ObjectRef {
	t.Helper()
	spec := map[string]any{"ingressClassName": "nginx", "rules": rules}
	if tlsHosts != nil {
		spec["tls"] = []any{map[string]any{"hosts": tlsHosts, "secretName": name + "-tls"}}
	}
	obj := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "networking.k8s.io/v1", "kind": "Ingress",
		"metadata": map[string]any{"name": name, "namespace": "apps", "uid": "uid-ing-" + name, "creationTimestamp": ts(40 * 24 * time.Hour)},
		"spec":     spec}}
	p, _ := kinds.ByKey("ingresses")
	ref := model.ObjectRef{Context: "demo", Kind: "ingresses", Namespace: "apps", Name: name}
	m, err := engine.BuildDetailWith(p, ref, obj, nil, fixedNow, model.NoRelated)
	if err != nil {
		t.Fatal(err)
	}
	f.details[ref.String()] = m
	return ref
}

// ingressPath is a rule's path to a Service's port, a number or a name.
func ingressPath(path, typ, service string, port any) any {
	p := map[string]any{"name": port}
	if n, ok := port.(int64); ok {
		p = map[string]any{"number": n}
	}
	return map[string]any{"path": path, "pathType": typ, "backend": map[string]any{"service": map[string]any{"name": service, "port": p}}}
}

// TestOpenWebLinkChecksTheAddress: an address from the cluster opens only as an absolute http or
// https URL with a host and without user information; anything else is a warning, and nothing
// opens.
func TestOpenWebLinkChecksTheAddress(t *testing.T) {
	opened := stubBrowser(t)
	f := ingressFake(t)
	sh, _, q := settledShell(t, f, "ingresses")
	for _, raw := range []string{"javascript:alert(1)", "file:///etc/passwd", "https://user:pw@shop.example.com", "//shop.example.com", "http://"} {
		toasts := len(sh.toasts.Objects)
		sh.openWebLink(raw)
		q.until(t, "the warning for "+raw, func() bool { return len(sh.toasts.Objects) > toasts })
	}
	sh.openWebLink("http://shop.example.com")
	if !slices.Equal(*opened, []string{"http://shop.example.com"}) {
		t.Errorf("opened %q", *opened)
	}
}

// TestRenderIngressesDark: the Ingresses view, the pointer on storefront's second host.
func TestRenderIngressesDark(t *testing.T) {
	f := ingressFake(t)
	sh, w, q := settledShell(t, f, "ingresses")
	sh.tip.delay = 0
	c := cellAt(t, sh, rowOf(t, f, "ingresses", "storefront"), columnOf(t, sh, "hosts"))
	test.MoveMouse(w.Canvas(), runAt(c.more[1]))
	q.until(t, "the address's tooltip", func() bool { return sh.tip.visibleText() == "https://www.shop.example.com" })
	savePNG(t, w, "ingresses_dark.png")
}

// TestRenderIngressDrawerDark: an Ingress's drawer with two rules: one host with four paths to two
// Services, its paths of different lengths and types, and one with a path the controller reads as
// a regular expression, which is no link; the pointer on the first path.
func TestRenderIngressDrawerDark(t *testing.T) {
	f := ingressFake(t)
	ref := withIngressDetail(t, f, "workspace", []any{"workspace.k8s-main.example.net"},
		map[string]any{"host": "workspace.k8s-main.example.net", "http": map[string]any{"paths": []any{
			ingressPath("/api/", "Prefix", "workspace-backend", "http"),
			ingressPath("/auth/", "Prefix", "workspace-backend", "http"),
			ingressPath("/healthz", "Exact", "workspace-backend", int64(8080)),
			ingressPath("/", "Prefix", "workspace-frontend", "http")}}},
		map[string]any{"host": "legacy.example.net", "http": map[string]any{"paths": []any{
			ingressPath("/old(/|$)(.*)", "ImplementationSpecific", "workspace-frontend", int64(80))}}})
	f.ui.Selected, f.ui.Drawer = []model.ObjectRef{ref}, &ref
	sh, w, q := newTestShell(t, f, Options{})
	q.until(t, "the drawer", func() bool { return sh.drawer.last != nil && len(sh.drawer.details.Objects) > 2 && sh.diag.total > 0 })
	link := drawerLinks(sh)["/api/"]
	if link == nil {
		t.Fatalf("no link on the path: %q", drawerTexts(sh))
	}
	link.MouseIn(&desktop.MouseEvent{})
	savePNG(t, w, "ingress_drawer_dark.png")
}
