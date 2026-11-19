package commands_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/guided-traffic/cogpit/internal/cache"
	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

const gatewayKey = "gateways.gateway.networking.k8s.io"

var gatewaysGVR = schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "gateways"}

// crdConnector is fakeConnector with the custom kinds watch on.
type crdConnector struct{ src *testkit.MemorySource }

func (c crdConnector) Contexts() ([]engine.ContextInfo, error) {
	return []engine.ContextInfo{{Name: "test"}}, nil
}

func (c crdConnector) Connect(context.Context, string) (*engine.Backend, error) {
	ch := cache.New(c.src, cache.Options{WarmPeriod: time.Minute})
	return &engine.Backend{Context: "test", Namespace: "default", Cache: ch, Profiles: testProfiles(),
		CustomResources: true, Close: ch.Close}, nil
}

func gateway(namespace, name, class string) *unstructured.Unstructured {
	return testkit.Object("gateway.networking.k8s.io/v1", "Gateway", namespace, name, map[string]any{
		"spec": map[string]any{"gatewayClassName": class},
		"status": map[string]any{
			"addresses":  []any{map[string]any{"type": "IPAddress", "value": "203.0.113.7"}},
			"conditions": []any{map[string]any{"type": "Programmed", "status": "True"}},
		},
	})
}

func gatewayCRD() *unstructured.Unstructured {
	return testkit.CRD("gateway.networking.k8s.io", "gateways", "Gateway", true,
		testkit.CRDVersion{Name: "v1beta1", Served: true},
		testkit.CRDVersion{Name: "v1", Served: true, Storage: true, Columns: []map[string]any{
			{"name": "Class", "type": "string", "jsonPath": ".spec.gatewayClassName"},
			{"name": "Address", "type": "string", "jsonPath": ".status.addresses[*].value"},
			{"name": "Programmed", "type": "string", "jsonPath": `.status.conditions[?(@.type=="Programmed")].status`},
			{"name": "Age", "type": "date", "jsonPath": ".metadata.creationTimestamp"},
		}})
}

func newCRDHarness(t *testing.T, src *testkit.MemorySource) *harness {
	t.Helper()
	h := &harness{src: src, writer: &fakeWriter{}}
	h.s = engine.New(engine.Options{Connector: crdConnector{src: src}, SnapshotInterval: 5 * time.Millisecond})
	if err := commands.RegisterAll(h.s.Registry()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.s.Close)
	if err := h.s.Connect(context.Background(), "test"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if !testkit.WaitFor(ctx, func() bool { _, ok := h.s.Backend().Profile(gatewayKey); return ok }) {
		t.Fatal("the Gateway kind never appeared")
	}
	return h
}

// TestCustomKindsThroughCommands drives a custom resource kind the way an agent does: kinds_list
// names it, resources_list and resources_find read it from the cache, ui_navigate shows it, and
// ui_state reports when the cluster stops serving it.
func TestCustomKindsThroughCommands(t *testing.T) {
	src := seed()
	src.Set(testkit.CRDsGVR, gatewayCRD())
	src.Set(gatewaysGVR, gateway("ingress", "public", "envoy"))
	src.Set(gatewaysGVR, gateway("ingress", "internal", "istio"))
	h := newCRDHarness(t, src)

	var kinds []commands.KindInfo
	h.must(t, engine.OriginExternalAgent, "kinds.list", struct{}{}, &kinds)
	last := kinds[len(kinds)-1]
	if last.Kind != gatewayKey || last.Title != "Gateway" || last.APIKind != "Gateway" || last.Version != "v1" ||
		last.Group != "gateway.networking.k8s.io" || !last.Namespaced {
		t.Fatalf("kinds.list's custom kind: %+v", last)
	}
	if kinds[0].Kind != "pods" {
		t.Errorf("built-in kinds come first, got %s", kinds[0].Kind)
	}
	var cols []string
	for _, c := range last.Columns {
		cols = append(cols, c.ID)
	}
	if strings.Join(cols, ",") != "name,namespace,class,address,programmed,age" {
		t.Errorf("columns %v", cols)
	}

	// An agent's search across kinds reaches a custom kind only once it is cached.
	if err := h.run(t, engine.OriginExternalAgent, "resources.find", map[string]any{"field": "class", "value": "envoy"}, nil); err == nil ||
		!strings.Contains(err.Error(), "no kind has a field") {
		t.Errorf("find across kinds before the kind is cached: %v", err)
	}

	var list commands.ListResult
	h.must(t, engine.OriginExternalAgent, "resources.list", map[string]any{"kind": gatewayKey}, &list)
	if list.Total != 2 || list.RefAt(0).Name != "internal" || list.Cell(1, "Class") != "envoy" ||
		list.Cell(1, "Address") != "203.0.113.7" || list.Cell(1, "Programmed") != "True" {
		t.Fatalf("resources.list: %+v", list)
	}

	var found commands.FindResult
	h.must(t, engine.OriginExternalAgent, "resources.find", map[string]any{"field": "class", "value": "envoy"}, &found)
	want := model.ObjectRef{Context: "test", Kind: gatewayKey, Namespace: "ingress", Name: "public"}
	if found.Count != 1 || found.Matches[0].Ref != want {
		t.Fatalf("resources.find across cached kinds: %+v", found)
	}

	var nav commands.NavigateResult
	h.must(t, engine.OriginExternalAgent, "ui.navigate", map[string]any{"ref": found.Matches[0].Ref}, &nav)
	if u := h.s.UIState(); !nav.Found || u.View != gatewayKey || u.Drawer == nil || u.Drawer.Name != "public" {
		t.Fatalf("ui.navigate: %+v, state view %s drawer %v", nav, u.View, u.Drawer)
	}
	var st commands.StateResult
	h.must(t, engine.OriginExternalAgent, "ui.state", struct{}{}, &st)
	if st.ViewTitle != "Gateway" || st.ViewError != "" {
		t.Errorf("ui.state: title %q error %q", st.ViewTitle, st.ViewError)
	}

	// The definition is deleted: the view stays and says why it is empty.
	src.Delete(testkit.CRDsGVR, "", gatewayKey)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if !testkit.WaitFor(ctx, func() bool {
		st = commands.StateResult{}
		return h.run(t, engine.OriginExternalAgent, "ui.state", struct{}{}, &st) == nil && st.ViewError != ""
	}) {
		t.Fatalf("ui.state never reported the vanished kind: %+v", st)
	}
	if !strings.Contains(st.ViewError, "no longer serves Gateway") || st.View != gatewayKey {
		t.Errorf("ui.state: %+v", st)
	}
	err := h.run(t, engine.OriginExternalAgent, "ui.openView", map[string]any{"view": gatewayKey}, nil)
	if err == nil || !strings.Contains(err.Error(), "this cluster serves") {
		t.Errorf("opening the vanished kind: %v", err)
	}
}
