package fyneui

import (
	"slices"
	"testing"

	"fyne.io/fyne/v2"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
)

// outdatedFake is a connected cluster that serves Replication Controllers and Endpoints beside
// Deployments and EndpointSlices.
func outdatedFake(t *testing.T) *fakeEngine {
	f := crdFake(t)
	f.kinds = append(f.kinds,
		kindInfo{Kind: "replicationcontrollers", Title: "Replication Controllers", APIKind: "ReplicationController", Version: "v1", Namespaced: true},
		kindInfo{Kind: "endpoints", Title: "Endpoints", APIKind: "Endpoints", Version: "v1", Namespaced: true},
		kindInfo{Kind: "endpointslices", Title: "Endpoint Slices", APIKind: "EndpointSlice", Group: "discovery.k8s.io", Version: "v1", Namespaced: true})
	p := engine.DefaultPreferences()
	f.ui.Preferences = &p
	return f
}

// The navigator leaves the outdated kinds out by default, its search finds them, the palette lists
// them, and the View menu's Hide Outdated Objects, checked, turns the preference off through
// preferences.set; with it off the tree lists them.
func TestNavigatorHidesOutdatedKinds(t *testing.T) {
	f := outdatedFake(t)
	sh, _, q := newTestShell(t, f, Options{})
	q.until(t, "kinds", func() bool { return len(sh.kinds) > 0 })
	s := sh.side
	s.setOpen(groupPrefix+"workloads", true)
	s.setOpen(groupPrefix+"network", true)
	shown := s.shownNodes()
	for _, k := range engine.OutdatedKinds {
		if slices.Contains(shown, k) || slices.Contains(s.tree.visible, k) {
			t.Errorf("%s shows with the preference on", k)
		}
	}
	if !slices.Contains(shown, "deployments") || !slices.Contains(shown, "endpointslices") {
		t.Errorf("the open groups show %v", shown)
	}
	// An outdated view opened elsewhere (the palette, a link) is marked nowhere.
	s.selectView("endpoints")
	if s.marked != "" {
		t.Errorf("the hidden Endpoints is marked: %q", s.marked)
	}

	// The search finds them.
	s.setFilter("endpoints")
	if shown := s.shownNodes(); !slices.Contains(shown, "endpoints") {
		t.Errorf("the search for endpoints shows %v", shown)
	}
	s.setFilter("")
	var views []string
	for _, v := range s.paletteViews() {
		views = append(views, v.View)
	}
	for _, k := range engine.OutdatedKinds {
		if !slices.Contains(views, k) {
			t.Errorf("the palette does not list %s: %v", k, views)
		}
	}

	item := sh.outdatedMenuItem()
	if !item.Checked || item.Label != outdatedMenuLabel {
		t.Fatalf("the View menu's item: %q, checked %v", item.Label, item.Checked)
	}
	item.Action()
	q.until(t, "preferences.set", func() bool { return len(f.callsOf("preferences.set")) == 1 })
	if p := decodeParams[map[string]bool](t, f.callsOf("preferences.set")[0]); p["hideOutdatedObjects"] {
		t.Errorf("preferences.set %v", p)
	}
	f.update(func(u *engine.UIState) {
		p := engine.DefaultPreferences()
		p.HideOutdatedObjects = false
		u.Preferences = &p
	})
	q.until(t, "the outdated kinds in the tree", func() bool {
		return slices.Contains(s.tree.visible, "replicationcontrollers") && slices.Contains(s.tree.visible, "endpoints")
	})
	if sh.outdatedMenuItem().Checked {
		t.Error("the View menu's item is checked with the preference off")
	}
}

// The navigator of a cluster that runs Flux and Argo CD: the heading Integrations below Other, Flux
// CD open under it, Argo CD closed. PNG: testdata/navigator_integrations_dark.png.
func TestRenderNavigatorIntegrationsDark(t *testing.T) {
	f := outdatedFake(t)
	f.kinds = append(f.kinds, fluxKindInfos()...)
	for _, p := range kinds.Curated() {
		if p.Group == kinds.ArgoCDGroup {
			f.kinds = append(f.kinds, kindInfo{Kind: p.Key, Title: p.Title, APIKind: p.Kind, Group: p.Group, Version: p.Version, Namespaced: p.Namespaced})
		}
	}
	snap := orderSnapshot()
	f.ui.View = snap.View
	f.tables[snap.View] = snap
	f.diag.Context, f.diag.Demo, f.diag.DemoStats = "kind-cogpit", false, nil
	sh, w, q := newTestShell(t, f, Options{Theme: ThemeDark})
	q.until(t, "the shell to settle", func() bool {
		return len(sh.kinds) > 0 && sh.diag.total > 0 && sh.side.marked == snap.View
	})
	sh.side.tree.CloseBranch(customGroup)
	sh.side.tree.OpenBranch(fluxGroup)
	w.Resize(fyne.NewSize(1440, 1080))
	savePNG(t, w, "navigator_integrations_dark.png")
}
