package engine

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// TestBuildDetailPodAsLens checks the drawer model of a pod against Lens's drawer (capture): one
// Properties section first (metadata merged with the pod's state, no UID, labels folded behind a
// count), the volumes and containers after it, the events last.
func TestBuildDetailPodAsLens(t *testing.T) {
	p, _ := kinds.ByKey("pods")
	pod := testkit.Pod("shop", "web-0")
	pod.SetLabels(map[string]string{"app": "web", "tier": "front"})
	pod.SetAnnotations(map[string]string{"note": "x"})
	pod.Object["metadata"].(map[string]any)["ownerReferences"] = []any{
		map[string]any{"apiVersion": "apps/v1", "kind": "ReplicaSet", "name": "web-5d8f", "uid": "u", "controller": true},
	}
	pod.Object["spec"].(map[string]any)["initContainers"] = []any{map[string]any{"name": "migrate", "image": "m:1"}}
	base := time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC)
	events := []*unstructured.Unstructured{
		event("shop", "e1", "Pod", "web-0", "uid-shop-web-0", "Scheduled", "Normal", base),
		event("shop", "e2", "Pod", "web-0", "uid-shop-web-0", "BackOff", "Warning", base.Add(time.Minute)),
	}
	ref := model.ObjectRef{Context: "test", Kind: "pods", Namespace: "shop", Name: "web-0"}
	m, err := BuildDetail(p, ref, pod, events, base.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	var titles []string
	for _, s := range m.Sections {
		titles = append(titles, s.Title)
	}
	if got := strings.Join(titles, ","); got != "Properties,migrate,app" {
		t.Fatalf("sections %s", got)
	}
	var names []string
	items := map[string]model.DetailItem{}
	for _, it := range m.Sections[0].Items {
		names = append(names, it.Name)
		items[it.Name] = it
	}
	if got := strings.Join(names, ","); got != "Created,Name,Namespace,Labels,Annotations,Controlled By,Status,Node,Pod IP,Pod IPs" {
		t.Errorf("properties %s", got)
	}
	if _, uid := items["UID"]; uid {
		t.Error("the drawer shows the UID")
	}
	if l := items["Labels"]; l.Fold != "Labels" || l.Entries() != 2 || len(l.Chips) != 2 {
		t.Errorf("labels %+v", l)
	}
	if c := items["Controlled By"]; c.Link == nil || *c.Link != (model.ObjectRef{Kind: "replicasets", Namespace: "shop", Name: "web-5d8f"}) {
		t.Errorf("controlled by %+v", c)
	}
	if n := items["Node"]; n.Link == nil || n.Link.Kind != "nodes" || n.Link.Name != "node-1" {
		t.Errorf("node %+v", n)
	}
	if len(m.Events) != 2 || m.Events[0].Reason != "BackOff" {
		t.Errorf("events %+v", m.Events)
	}
	if strings.Join(m.Containers, ",") != "app" || strings.Join(m.InitContainers, ",") != "migrate" {
		t.Errorf("containers %v init %v", m.Containers, m.InitContainers)
	}
	// In the JSON agents read, the events come after the sections, as in the drawer.
	raw, _ := json.Marshal(m)
	if s := string(raw); strings.Index(s, `"sections"`) > strings.Index(s, `"events"`) {
		t.Errorf("events before sections in %s", s)
	}
}

func TestBuildDetailNodeTables(t *testing.T) {
	p, _ := kinds.ByKey("nodes")
	node := testkit.Object("v1", "Node", "", "worker-1", map[string]any{
		"spec": map[string]any{"unschedulable": true},
		"status": map[string]any{
			"conditions": []any{map[string]any{"type": "Ready", "status": "True"}},
			"nodeInfo":   map[string]any{"kubeletVersion": "v1.37.1", "osImage": "Example Linux"},
			"capacity":   map[string]any{"cpu": "4", "memory": "8Gi", "pods": "110"},
		},
	})
	m, err := BuildDetail(p, model.ObjectRef{Context: "test", Kind: "nodes", Name: "worker-1"}, node, nil, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Sections) != 2 || m.Sections[0].Title != model.TitleProperties || m.Sections[1].Title != "Capacity" || m.Sections[1].Table == nil ||
		m.Sections[0].Items[0].Name != "Created" || m.Sections[0].Items[1].Name != "Name" {
		t.Fatalf("sections %+v", m.Sections)
	}
	if m.Status != model.StatusOK {
		t.Errorf("a cordoned Ready node is ok, got %q", m.Status)
	}
	if len(m.Events) != 0 || m.Events == nil {
		t.Errorf("events %v: want an empty list, never null", m.Events)
	}
}

func TestBuildDetailSurvivesAPanickingProfile(t *testing.T) {
	p := *testPods
	p.Detail = func(*unstructured.Unstructured) []model.DetailSection { panic("boom") }
	m, err := BuildDetail(&p, model.ObjectRef{Kind: "pods", Namespace: "shop", Name: "web-0"}, testkit.Pod("shop", "web-0"), nil, time.Now())
	if err == nil || len(m.Sections) != 1 || m.Sections[0].Title != model.TitleProperties {
		t.Errorf("panicking profile: %v %+v", err, m.Sections)
	}
}
