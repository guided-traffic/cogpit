package kinds_test

import (
	"slices"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
)

// servicePod returns a pod of shop with web's selector labels (match) or another app's.
func servicePod(name, uid string, match bool, status map[string]any) *unstructured.Unstructured {
	labels := map[string]any{"app.kubernetes.io/name": "web", "app.kubernetes.io/component": "frontend", "pod-template-hash": "7d9f8c6b5"}
	if !match {
		labels = map[string]any{"app.kubernetes.io/name": "web"} // half the selector
	}
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1", "kind": "Pod",
		"metadata": map[string]any{"name": name, "namespace": "shop", "uid": uid, "labels": labels},
		"status":   status,
	}}
}

// webSlice returns an endpoint slice of shop/web with endpoints for the given pods: name, uid,
// ready, terminating.
func webSlice(name string, eps ...[4]any) *unstructured.Unstructured {
	var endpoints []any
	for _, e := range eps {
		endpoints = append(endpoints, map[string]any{
			"addresses":  []any{"10.244.1.5"},
			"conditions": map[string]any{"ready": e[2], "terminating": e[3]},
			"targetRef":  map[string]any{"kind": "Pod", "namespace": "shop", "name": e[0], "uid": e[1]},
		})
	}
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "discovery.k8s.io/v1", "kind": "EndpointSlice",
		"metadata": map[string]any{
			"name": name, "namespace": "shop",
			"labels": map[string]any{"kubernetes.io/service-name": "web"},
		},
		"addressType": "IPv4",
		"endpoints":   endpoints,
	}}
}

func TestServicePods(t *testing.T) {
	p, _ := kinds.ByKey("services")
	svc := recorded(t, "services", "healthy") // shop/web
	running := map[string]any{"phase": "Running", "podIP": "10.244.1.5"}
	ipv4 := webSlice("web-v4",
		[4]any{"web-a", "uid-a", true, false},
		[4]any{"web-b", "uid-b", false, false},
		[4]any{"web-c", "uid-c", false, true},
		[4]any{"web-old", "uid-gone", true, false}, // a pod of the name before it was created again
	)
	ipv6 := webSlice("web-v6", [4]any{"web-b", "uid-b", true, false}) // web-b ready over IPv6
	other := servicePod("web-a", "uid-other", true, running)
	other.SetNamespace("staging")
	rel := fakeRelated{
		"pods": {
			servicePod("web-c", "uid-c", true, running),
			servicePod("web-a", "uid-a", true, running),
			servicePod("web-b", "uid-b", true, running),
			servicePod("web-d", "uid-d", true, running),
			servicePod("web-e", "uid-e", true, map[string]any{"phase": "Pending"}),
			servicePod("web-f", "uid-f", true, map[string]any{"phase": "Succeeded", "podIP": "10.244.1.9"}),
			servicePod("web-old", "uid-new", true, running),
			servicePod("api-1", "uid-api", false, running),
			other,
		},
		"endpointslices": {ipv4, ipv6},
	}
	sections := p.DetailSections(svc, rel)
	titles := make([]string, 0, len(sections))
	for _, s := range sections {
		titles = append(titles, s.Title)
	}
	if i, j := slices.Index(titles, "Pods"), slices.Index(titles, "Endpoint Slices"); i < 0 || j != i+1 {
		t.Fatalf("Pods not right before Endpoint Slices: %q", titles)
	}
	tbl := findTable(t, sections, "Pods")
	want := [][]string{
		{"web-a", "Ready"},
		{"web-b", "Ready"},
		{"web-c", "Not ready (terminating)"},
		{"web-d", "Not ready (in no endpoint slice)"},
		{"web-e", "Not ready (no IP yet)"},
		{"web-f", "Not ready (Succeeded)"},
		{"web-old", "Not ready (in no endpoint slice)"},
	}
	if !slices.EqualFunc(tbl.Rows, want, slices.Equal[[]string]) {
		t.Errorf("rows %q, want %q", tbl.Rows, want)
	}
	if l := tbl.Cells[0][0].Link; l == nil || *l != (model.ObjectRef{Kind: "pods", Namespace: "shop", Name: "web-a"}) {
		t.Errorf("pod link %+v", l)
	}
	if c := tbl.Cells[0][1]; c.Status != model.StatusOK || !c.Tint {
		t.Errorf("Ready cell %+v, want ok and tinted", c)
	}
	if c := tbl.Cells[3][1]; c.Status != model.StatusError || !c.Tint {
		t.Errorf("Not ready cell %+v, want error and tinted", c)
	}

	// Slices not known yet: the pods are listed, their Ready unknown.
	tbl = findTable(t, p.DetailSections(svc, fakeRelated{"pods": rel["pods"]}), "Pods")
	if got := tbl.Rows[0][1]; got != "Unknown" {
		t.Errorf("Ready without slices %q, want Unknown", got)
	}
}

func TestServicePodsNotes(t *testing.T) {
	p, _ := kinds.ByKey("services")
	svc := recorded(t, "services", "healthy")
	note := func(svc *unstructured.Unstructured, rel model.Related) string {
		t.Helper()
		for _, s := range p.DetailSections(svc, rel) {
			if s.Title == "Pods" {
				if s.Table != nil || len(s.Items) != 1 {
					t.Fatalf("Pods section %+v, want one note", s)
				}
				return s.Items[0].Value
			}
		}
		return ""
	}
	if got := note(svc, fakeRelated{"pods": {servicePod("api-1", "uid-api", false, nil)}}); got != "No pods match the selector" {
		t.Errorf("no match: %q", got)
	}
	if got := note(svc, fakeRelated{}); got != "" {
		t.Errorf("pods still listing: %q, want no section", got)
	}
	bare := svc.DeepCopy()
	unstructured.RemoveNestedField(bare.Object, "spec", "selector")
	if got := note(bare, fakeRelated{}); got != "No selector: Kubernetes picks no pods for this Service; its endpoints are in Endpoint Slices." {
		t.Errorf("no selector: %q", got)
	}
	ext := recorded(t, "services", "externalname")
	if got := note(ext, fakeRelated{}); got != "ExternalName: traffic goes to orders-db.example.net, no pods." {
		t.Errorf("ExternalName: %q", got)
	}
}
