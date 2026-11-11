package fyneui

import (
	"reflect"
	"testing"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// A Service's drawer with its Pods section, over the real engine: the pods its selector matches,
// Ready from its endpoint slice, before the Endpoint Slices section. PNG: drawer_service_pods_dark.png.

var renderSlicesGVR = schema.GroupVersionResource{Group: "discovery.k8s.io", Version: "v1", Resource: "endpointslices"}

func servicePodsFixture() *testkit.MemorySource {
	src := testkit.NewMemorySource()
	pod := func(name string, labels map[string]string, ip string) {
		p := testkit.Pod("shop", name)
		p.SetLabels(labels)
		p.SetUID(types.UID("uid-" + name))
		if ip == "" {
			p.Object["status"] = map[string]any{"phase": "Pending"}
		}
		src.Set(testkit.PodsGVR, p)
	}
	web := map[string]string{"app": "web"}
	pod("web-7d9f8c6b5-bkmqx", web, "10.244.1.5")
	pod("web-7d9f8c6b5-clnry", web, "10.244.2.7")
	pod("web-7d9f8c6b5-dmosz", web, "")
	pod("blog-0", map[string]string{"app": "blog"}, "10.244.3.9")
	src.Set(testkit.ServicesGVR, testkit.Object("v1", "Service", "shop", "web", map[string]any{
		"spec": map[string]any{"selector": map[string]any{"app": "web"}, "ports": []any{map[string]any{"port": int64(80), "protocol": "TCP"}}}}))
	endpoint := func(name, ip string, ready bool) any {
		return map[string]any{
			"addresses":  []any{ip},
			"conditions": map[string]any{"ready": ready},
			"targetRef":  map[string]any{"kind": "Pod", "namespace": "shop", "name": name, "uid": "uid-" + name},
		}
	}
	slice := testkit.Object("discovery.k8s.io/v1", "EndpointSlice", "shop", "web-x7k2p", map[string]any{
		"addressType": "IPv4",
		"endpoints":   []any{endpoint("web-7d9f8c6b5-bkmqx", "10.244.1.5", true), endpoint("web-7d9f8c6b5-clnry", "10.244.2.7", false)},
		"ports":       []any{map[string]any{"port": int64(8080), "protocol": "TCP"}},
	})
	slice.SetLabels(map[string]string{"kubernetes.io/service-name": "web"})
	src.Set(renderSlicesGVR, slice)
	return src
}

func TestRenderServicePodsDark(t *testing.T) {
	s := ephemeralEngine(t, servicePodsFixture(), quietWriter{})
	sh, w, q := newEngineShell(t, s, Options{AutoConnect: "kind-cogpit", Theme: ThemeDark})
	q.until(t, "the connection", func() bool { return sh.ui.Connection.State == engine.Connected && len(sh.kinds) > 0 })
	mustDispatch(t, s, "ui.openView", map[string]any{"view": "services"})
	ref := model.ObjectRef{Context: "kind-cogpit", Kind: "services", Namespace: "shop", Name: "web"}
	mustDispatch(t, s, "ui.select", map[string]any{"refs": []model.ObjectRef{ref}})
	want := [][]string{
		{"web-7d9f8c6b5-bkmqx", "Ready"},
		{"web-7d9f8c6b5-clnry", "Not ready"},
		{"web-7d9f8c6b5-dmosz", "Not ready (no IP yet)"},
	}
	q.until(t, "the drawer's Pods", func() bool {
		m := sh.drawer.last
		if m == nil {
			return false
		}
		for _, sec := range m.Sections {
			if sec.Title == "Pods" && sec.Table != nil && reflect.DeepEqual(sec.Table.Rows, want) {
				return true
			}
		}
		return false
	})
	settle(w)
	scrollDrawerTo(t, sh, "Pods")
	settle(w)
	savePNG(t, w, "drawer_service_pods_dark.png")
}
