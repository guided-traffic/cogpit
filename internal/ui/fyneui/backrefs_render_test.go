package fyneui

import (
	"reflect"
	"testing"

	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// A pod's drawer with Preferences › App › Back references on (D-152), over the real engine: the
// Services, Network Policies and Pod Disruption Budget that select web-0, as the drawer's last
// section. PNG: drawer_back_references_dark.png.

var (
	renderPoliciesGVR = schema.GroupVersionResource{Group: "networking.k8s.io", Version: "v1", Resource: "networkpolicies"}
	renderBudgetsGVR  = schema.GroupVersionResource{Group: "policy", Version: "v1", Resource: "poddisruptionbudgets"}
)

func backRefsFixture() *testkit.MemorySource {
	src := testkit.NewMemorySource()
	pod := testkit.Pod("shop", "web-0")
	pod.SetLabels(map[string]string{"app": "web", "tier": "front"})
	src.Set(testkit.PodsGVR, pod)
	service := func(name string, selector map[string]any) {
		src.Set(testkit.ServicesGVR, testkit.Object("v1", "Service", "shop", name, map[string]any{
			"spec": map[string]any{"selector": selector, "ports": []any{map[string]any{"port": int64(80), "protocol": "TCP"}}}}))
	}
	service("web", map[string]any{"app": "web"})
	service("web-front", map[string]any{"app": "web", "tier": "front"})
	service("blog", map[string]any{"app": "blog"})
	policy := func(name string, selector map[string]any) {
		src.Set(renderPoliciesGVR, testkit.Object("networking.k8s.io/v1", "NetworkPolicy", "shop", name, map[string]any{
			"spec": map[string]any{"podSelector": selector, "policyTypes": []any{"Ingress"}}}))
	}
	policy("default-deny", map[string]any{})
	policy("allow-web", map[string]any{"matchLabels": map[string]any{"app": "web"}})
	policy("allow-blog", map[string]any{"matchLabels": map[string]any{"app": "blog"}})
	src.Set(renderBudgetsGVR, testkit.Object("policy/v1", "PodDisruptionBudget", "shop", "web-pdb", map[string]any{
		"spec": map[string]any{"minAvailable": int64(1), "selector": map[string]any{"matchLabels": map[string]any{"app": "web"}}}}))
	return src
}

func renderBackRefs(t *testing.T, mode ThemeMode, name string) {
	s := ephemeralEngine(t, backRefsFixture(), quietWriter{})
	if _, err := s.ChangePreferences(func(p *engine.Preferences) error { p.BackReferences = true; return nil }); err != nil {
		t.Fatal(err)
	}
	sh, w, q := newEngineShell(t, s, Options{AutoConnect: "kind-cogpit", Theme: mode})
	q.until(t, "the connection", func() bool { return sh.ui.Connection.State == engine.Connected && len(sh.kinds) > 0 })
	mustDispatch(t, s, "ui.openView", map[string]any{"view": "pods"})
	ref := model.ObjectRef{Context: "kind-cogpit", Kind: "pods", Namespace: "shop", Name: "web-0"}
	mustDispatch(t, s, "ui.select", map[string]any{"refs": []model.ObjectRef{ref}})
	want := [][]string{ // grouped by Kind until sorted otherwise
		{"NetworkPolicy", "allow-web", "spec.podSelector"},
		{"NetworkPolicy", "default-deny", "spec.podSelector (all pods)"},
		{"PodDisruptionBudget", "web-pdb", "spec.selector"},
		{"Service", "web", "spec.selector"},
		{"Service", "web-front", "spec.selector"},
	}
	var last model.DetailSection
	defer func() {
		if t.Failed() {
			t.Logf("last section %+v rows %q", last, func() [][]string {
				if last.Table == nil {
					return nil
				}
				return last.Table.Rows
			}())
		}
	}()
	q.until(t, "the drawer's Referenced By", func() bool {
		m := sh.drawer.last
		if m == nil || len(m.Sections) == 0 {
			return false
		}
		last = m.Sections[len(m.Sections)-1]
		if last.Table != nil {
			last.Table = &model.DetailTable{Rows: last.Table.Rows}
		}
		return last.Title == kinds.TitleReferencedBy && last.Table != nil && reflect.DeepEqual(last.Table.Rows, want)
	})
	settle(w)
	scrollDrawerTo(t, sh, kinds.TitleReferencedBy)
	settle(w)
	savePNG(t, w, name)
}

func TestRenderBackRefsDark(t *testing.T) {
	renderBackRefs(t, ThemeDark, "drawer_back_references_dark.png")
}
