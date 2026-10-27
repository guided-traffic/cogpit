package fyneui

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// customKinds are the kinds of a cluster with cert-manager, the Gateway API and a Vertical Pod
// Autoscaler installed (synthetic), as kinds.list reports them: key, Kind, group.
func customKinds() []kindInfo {
	k := func(key, kind string) kindInfo {
		_, group, _ := strings.Cut(key, ".")
		return kindInfo{Kind: key, Title: kind, APIKind: kind, Group: group, Version: "v1", Namespaced: true}
	}
	return []kindInfo{
		k("orders.acme.cert-manager.io", "Order"),
		k("challenges.acme.cert-manager.io", "Challenge"),
		k("issuers.cert-manager.io", "Issuer"),
		k("certificates.cert-manager.io", "Certificate"),
		k("clusterissuers.cert-manager.io", "ClusterIssuer"),
		k("certificaterequests.cert-manager.io", "CertificateRequest"),
		k("httproutes.gateway.networking.k8s.io", "HTTPRoute"),
		k("gateways.gateway.networking.k8s.io", "Gateway"),
		k("referencegrants.gateway.networking.k8s.io", "ReferenceGrant"),
		k("gatewayclasses.gateway.networking.k8s.io", "GatewayClass"),
		k("tcproutes.gateway.networking.k8s.io", "TCPRoute"),
		k("verticalpodautoscalers.autoscaling.k8s.io", "VerticalPodAutoscaler"),
	}
}

func crdFake(t testing.TB) *fakeEngine {
	f := connectedFake(t, 200)
	f.ui.Context = "kind-cogpit"
	f.ui.Connection = engine.Connection{State: engine.Connected}
	f.kinds = append(append(fixtureKinds(),
		kindInfo{Kind: "customresourcedefinitions", Title: "Definitions", APIKind: "CustomResourceDefinition", Group: "apiextensions.k8s.io", Version: "v1"}),
		customKinds()...)
	return f
}

func titlesOf(s *sidebar, ids []string) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, s.titles[id])
	}
	return out
}

// TestSidebarCustomResources checks the tree against the captured sidebar: Network gains the
// Gateway API sub-group after Network Policies, Config shows Vertical Pod Autoscalers, and Custom
// Resources lists Definitions, then one closed group per API group with its kinds by singular
// Kind name, the curated sections' kinds included.
func TestSidebarCustomResources(t *testing.T) {
	f := crdFake(t)
	sh, _, q := newTestShell(t, f, Options{})
	q.until(t, "kinds", func() bool { return len(sh.kinds) > 0 })
	s := sh.side

	if got := titlesOf(s, s.children[""]); !slices.Equal(got, []string{"Overview", "Nodes", "Workloads", "Config", "Network", "Namespaces", "Events", "Custom Resources"}) {
		t.Errorf("top level %v", got)
	}
	if got := s.children[groupPrefix+"network"]; !slices.Equal(got, []string{"services", groupPrefix + "network/gatewayapi"}) {
		t.Errorf("network %v", got)
	}
	if got := titlesOf(s, s.children[groupPrefix+"network/gatewayapi"]); !slices.Equal(got,
		[]string{"Gateway Classes", "Gateways", "HTTP Routes", "Reference Grants", "TCP Routes"}) {
		t.Errorf("Gateway API %v", got)
	}
	if got := titlesOf(s, s.children[groupPrefix+"config"]); !slices.Equal(got, []string{"Vertical Pod Autoscalers"}) {
		t.Errorf("config %v", got)
	}
	custom := s.children[customGroup]
	if got := titlesOf(s, custom); !slices.Equal(got, []string{"Definitions", "acme.cert-manager.io", "autoscaling.k8s.io",
		"cert-manager.io", "gateway.networking.k8s.io"}) {
		t.Errorf("custom resources %v", got)
	}
	if got := titlesOf(s, s.children[customGroupPrefix+"cert-manager.io"]); !slices.Equal(got,
		[]string{"Certificate", "CertificateRequest", "ClusterIssuer", "Issuer"}) {
		t.Errorf("cert-manager.io %v", got)
	}
	// A curated section keeps its kinds in Custom Resources too, under an alias id.
	gw := s.children[customGroupPrefix+"gateway.networking.k8s.io"]
	if !slices.Equal(gw, []string{aliasPrefix + "gateways.gateway.networking.k8s.io", aliasPrefix + "gatewayclasses.gateway.networking.k8s.io",
		aliasPrefix + "httproutes.gateway.networking.k8s.io", aliasPrefix + "referencegrants.gateway.networking.k8s.io",
		aliasPrefix + "tcproutes.gateway.networking.k8s.io"}) || s.titles[gw[0]] != "Gateway" {
		t.Errorf("gateway.networking.k8s.io %v (%v)", gw, titlesOf(s, gw))
	}
	if s.children[otherGroup] != nil {
		t.Errorf("Other lists %v", s.children[otherGroup])
	}

	// Every group starts closed (owner, 2026-10-12), except the groups above the open view, which
	// open so that the selection is visible.
	above := map[string]bool{}
	for p := s.parents[sh.ui.View]; p != ""; p = s.parents[p] {
		above[p] = true
	}
	for id := range s.children {
		if id == "" {
			continue
		}
		if want := above[id]; s.tree.IsBranchOpen(id) != want {
			t.Errorf("%s open = %v, want %v (open view %q)", id, !want, want, sh.ui.View)
		}
	}

	// Selecting the alias opens the kind's view; the mark goes to the kind's node under Network.
	s.tree.Select(aliasPrefix + "gateways.gateway.networking.k8s.io")
	q.until(t, "the Gateways view", func() bool { return sh.ui.View == "gateways.gateway.networking.k8s.io" })
	calls := f.callsOf("ui.openView")
	var p openViewParams
	if err := json.Unmarshal(calls[len(calls)-1].Params, &p); err != nil || p.View != "gateways.gateway.networking.k8s.io" {
		t.Fatalf("ui.openView %s, %v", calls[len(calls)-1].Params, err)
	}
	if s.marked != "gateways.gateway.networking.k8s.io" {
		t.Errorf("selected node %q, want the Gateway API one", s.marked)
	}

	// An agent opens a kind inside a closed API group: the group opens to show the mark.
	f.update(func(u *engine.UIState) { u.View = "orders.acme.cert-manager.io" })
	q.until(t, "the Order view", func() bool { return sh.ui.View == "orders.acme.cert-manager.io" })
	if !s.tree.IsBranchOpen(customGroupPrefix+"acme.cert-manager.io") || s.marked != "orders.acme.cert-manager.io" {
		t.Errorf("acme.cert-manager.io open %v, selected %q", s.tree.IsBranchOpen(customGroupPrefix+"acme.cert-manager.io"), s.marked)
	}
}

// TestSidebarFollowsKindsEvent: EventKinds reloads kinds.list; a new API group appears closed, the
// groups the user opened stay open, and a group whose last kind is gone disappears.
func TestSidebarFollowsKindsEvent(t *testing.T) {
	f := crdFake(t)
	sh, _, q := newTestShell(t, f, Options{})
	q.until(t, "kinds", func() bool { return len(sh.kinds) > 0 })
	s := sh.side
	s.tree.OpenBranch(customGroupPrefix + "cert-manager.io")
	s.tree.CloseBranch(groupPrefix + "network")
	before := len(f.callsOf("kinds.list"))

	f.mu.Lock()
	f.kinds = slices.DeleteFunc(f.kinds, func(k kindInfo) bool { return strings.HasSuffix(k.Kind, ".acme.cert-manager.io") })
	f.kinds = append(f.kinds, kindInfo{Kind: "widgets.e2e.cogpit.test", Title: "Widget", APIKind: "Widget", Group: "e2e.cogpit.test", Version: "v1"})
	f.mu.Unlock()
	for range 3 { // a burst of events costs one reload at a time
		f.emit(engine.Event{Type: engine.EventKinds})
	}
	q.until(t, "the new kind", func() bool { return s.titles["widgets.e2e.cogpit.test"] == "Widget" })
	if n := len(f.callsOf("kinds.list")) - before; n < 1 || n > 3 {
		t.Errorf("%d kinds.list calls for a burst of three events", n)
	}
	if _, ok := s.children[customGroupPrefix+"acme.cert-manager.io"]; ok {
		t.Error("the group of the deleted kinds is still there")
	}
	if !s.tree.IsBranchOpen(customGroupPrefix+"cert-manager.io") || s.tree.IsBranchOpen(groupPrefix+"network") ||
		s.tree.IsBranchOpen(customGroupPrefix+"e2e.cogpit.test") {
		t.Error("a rebuild changed the branches' open state")
	}
	var viewMenu []string // loadKinds rebuilds the View menu too
	for _, it := range sh.mainMenu().Items[1].Items {
		viewMenu = append(viewMenu, it.Label)
	}
	if !slices.Contains(viewMenu, "Widget") || slices.Contains(viewMenu, "Order") {
		t.Errorf("View menu %v", viewMenu)
	}
}

// orderSnapshot is a Custom Resources view: cert-manager's Order with its captured columns.
func orderSnapshot() *engine.TableSnapshot {
	cols := []model.Column{
		{ID: "name", Title: "Name", Type: model.ColumnText},
		{ID: "namespace", Title: "Namespace", Type: model.ColumnText},
		{ID: "state", Title: "State", Type: model.ColumnText},
		{ID: "issuer", Title: "Issuer", Type: model.ColumnText, Hidden: true},
		{ID: "reason", Title: "Reason", Type: model.ColumnText, Hidden: true},
		{ID: "age", Title: "Age", Type: model.ColumnAge},
	}
	states := []string{"valid", "valid", "pending", "valid", "ready", "valid", "errored", "valid", "valid", "processing", "valid", "valid"}
	nss := []string{"storefront", "platform", "checkout", "identity", "payments", "search"}
	rows := make([]model.Row, 0, len(states))
	for i, st := range states {
		ns := nss[i%len(nss)]
		name := fmt.Sprintf("%s-tls-%d-%d", ns, i%3+1, 1849203857+i*7919)
		rows = append(rows, model.Row{
			Ref: model.ObjectRef{Context: "kind-cogpit", Kind: "orders.acme.cert-manager.io", Namespace: ns, Name: name},
			Cells: []model.Cell{{Text: name}, {Text: ns, Link: &model.ObjectRef{Kind: "namespaces", Name: ns}}, {Text: st},
				{Text: "letsencrypt-prod"}, {}, {Num: fixedNow.Add(-time.Duration(i*5+3) * time.Hour).Unix()}},
		})
	}
	slices.SortFunc(rows, func(a, b model.Row) int { return strings.Compare(a.Ref.Name, b.Ref.Name) })
	return &engine.TableSnapshot{View: "orders.acme.cert-manager.io", Context: "kind-cogpit", Title: "Order", Namespaced: true,
		Columns: cols, Rows: rows, Sort: engine.SortSpec{Column: "name"}, Total: len(rows), Revision: 1, Synced: true}
}

// customScene is the shell on a custom resource view, with the sidebar's Network › Gateway API
// and Custom Resources: the dark scene on cert-manager's Order with acme.cert-manager.io open,
// the light one on Gateways with gateway.networking.k8s.io open, where the Gateway API kinds are
// listed again by their Kind names.
func customScene(t *testing.T, mode ThemeMode, snap *engine.TableSnapshot, openGroup string) {
	f := crdFake(t)
	f.ui.View = snap.View
	f.tables[snap.View] = snap
	f.diag.Context = "kind-cogpit"
	f.diag.Demo = false
	f.diag.DemoStats = nil
	sh, w, q := newTestShell(t, f, Options{Theme: mode})
	q.until(t, "the shell to settle", func() bool {
		return len(sh.kinds) > 0 && sh.diag.total > 0 && sh.side.marked == snap.View
	})
	sh.side.tree.OpenBranch(customGroupPrefix + openGroup)
	w.Resize(fyne.NewSize(1440, 1080)) // tall enough for the whole tree
	savePNG(t, w, "sidebar_custom_"+string(mode)+".png")
}

func TestRenderSidebarCustomDark(t *testing.T) {
	customScene(t, ThemeDark, orderSnapshot(), "acme.cert-manager.io")
}
