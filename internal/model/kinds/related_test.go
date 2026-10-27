package kinds_test

import (
	"encoding/json"
	"slices"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	utiljson "k8s.io/apimachinery/pkg/util/json"

	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
)

// fakeRelated serves the related-objects indexes from objects by kind key, as the engine's index
// would: an object is found under every key its index's Keys returns.
type fakeRelated map[string][]*unstructured.Unstructured

// Listed: a kind the fake holds objects of is listed.
func (r fakeRelated) Listed(ix model.RelatedIndex) bool { _, ok := r[ix.Kind]; return ok }

func (r fakeRelated) Objects(ix model.RelatedIndex, key string) []*unstructured.Unstructured {
	var out []*unstructured.Unstructured
	for _, o := range r[ix.Kind] {
		if slices.Contains(ix.Keys(o), key) {
			out = append(out, o)
		}
	}
	return out
}

// recorded returns a copy of the recorded object testdata/<kind>/<name>.yaml.
func recorded(t testing.TB, kind, name string) *unstructured.Unstructured {
	t.Helper()
	for _, c := range loadCases(t, kind) {
		if c.name == name {
			return c.obj.DeepCopy()
		}
	}
	t.Fatalf("no testdata %s/%s", kind, name)
	return nil
}

// podOn returns a recorded pod renamed into namespace ns, on node, mounting claims.
func podOn(t testing.TB, recording, ns, name, node string, claims ...string) *unstructured.Unstructured {
	p := recorded(t, "pods", recording)
	p.SetNamespace(ns)
	p.SetName(name)
	_ = unstructured.SetNestedField(p.Object, node, "spec", "nodeName")
	vols := []any{}
	for _, c := range claims {
		vols = append(vols, map[string]any{"name": c, "persistentVolumeClaim": map[string]any{"claimName": c}})
	}
	_ = unstructured.SetNestedSlice(p.Object, vols, "spec", "volumes")
	return p
}

// TestPersistentVolumeClaimPods: the Pods column names the pods that mount the claim, in its
// namespace only; one pod is a link, several are text; the drawer links each.
func TestPersistentVolumeClaimPods(t *testing.T) {
	p, _ := kinds.ByKey("persistentvolumeclaims")
	claim := recorded(t, "persistentvolumeclaims", "healthy") // payments/data-postgres-0
	col := p.ColumnIndex("pods")
	pods := func(rel model.Related) model.Cell { return model.BuildRowWith(p, "test", claim, rel).Cells[col] }

	one := fakeRelated{"pods": {
		podOn(t, "healthy", "payments", "postgres-0", "worker-a1", "data-postgres-0"),
		podOn(t, "healthy", "orders", "postgres-0", "worker-a1", "data-postgres-0"), // same claim name, other namespace
		podOn(t, "healthy", "payments", "api-0", "worker-a1", "cache"),
	}}
	if c := pods(one); c.Text != "postgres-0" || c.Link == nil || *c.Link != (model.ObjectRef{Kind: "pods", Namespace: "payments", Name: "postgres-0"}) {
		t.Errorf("one pod: %+v", c)
	}
	two := fakeRelated{"pods": append(one["pods"], podOn(t, "failing", "payments", "backup-29311440-abcde", "worker-a2", "cache", "data-postgres-0"))}
	if c := pods(two); c.Text != "backup-29311440-abcde, postgres-0" || c.Link != nil {
		t.Errorf("two pods: %+v", c)
	}
	if c := pods(nil); c.Text != "" || c.Link != nil {
		t.Errorf("no related objects: %+v", c)
	}
	item := findItem(p.DetailSections(claim, two), "Pods")
	if len(item.Chips) != 2 || item.Chips[1].Link == nil || *item.Chips[1].Link != (model.ObjectRef{Kind: "pods", Namespace: "payments", Name: "postgres-0"}) {
		t.Errorf("drawer Pods %+v", item)
	}
	if it := findItem(p.DetailSections(claim, nil), "Pods"); it.Value != "" {
		t.Errorf("drawer without related objects has Pods %+v", it)
	}
}

// TestNodePodsTable: the node's Pods table after Allocatable, as captured (Name, warning icon,
// Node, Namespace, Ready, CPU, Memory, Status), one row per pod on the node, sorted by name, the
// name and namespace as links, CPU and Memory empty until metrics.
func TestNodePodsTable(t *testing.T) {
	p, _ := kinds.ByKey("nodes")
	node := recorded(t, "nodes", "healthy") // cp-1
	rel := fakeRelated{"pods": {
		podOn(t, "healthy", "shop", "web-0", "cp-1"),
		podOn(t, "failing", "jobs", "worker-0", "cp-1"),
		podOn(t, "healthy", "shop", "elsewhere", "worker-a1"),
	}}
	sections := p.DetailSections(node, rel)
	titles := make([]string, len(sections))
	for i, s := range sections {
		titles[i] = s.Title
	}
	if !slices.Equal(titles, []string{"Properties", "Capacity", "Allocatable", "Pods"}) {
		t.Fatalf("sections %q", titles)
	}
	tbl := sections[3].Table
	if !slices.Equal(tbl.Columns, []string{"Name", "Warning", "Node", "Namespace", "Ready", "CPU", "Memory", "Status"}) ||
		tbl.Types[1] != model.ColumnWarning {
		t.Errorf("columns %q, types %q", tbl.Columns, tbl.Types)
	}
	if len(tbl.Rows) != 2 {
		t.Fatalf("rows %q", tbl.Rows)
	}
	if !slices.Equal(tbl.Rows[0], []string{"web-0", "", "cp-1", "shop", "1/1", "", "", "Running"}) {
		t.Errorf("row 0 %q", tbl.Rows[0])
	}
	web, worker := tbl.Cells[0], tbl.Cells[1]
	if web[0].Link == nil || *web[0].Link != (model.ObjectRef{Kind: "pods", Namespace: "shop", Name: "web-0"}) || web[2].Link != nil ||
		web[3].Link == nil || web[7].Status != model.StatusOK {
		t.Errorf("web-0 cells %+v", web)
	}
	if worker[0].Text != "worker-0" || worker[1].Status != model.StatusWarning || worker[7].Text != "CrashLoopBackOff" || worker[7].Status != model.StatusError {
		t.Errorf("worker-0 cells %+v", worker)
	}
	if got := p.DetailSections(node, nil); len(got) != 3 {
		t.Errorf("without related objects: %d sections, want no Pods", len(got))
	}
}

// TestServiceEndpointSlices: a Service's drawer lists its endpoint slices, found by the
// kubernetes.io/service-name label in its namespace, as links.
func TestServiceEndpointSlices(t *testing.T) {
	p, _ := kinds.ByKey("services")
	svc := recorded(t, "services", "healthy") // shop/web
	other := recorded(t, "endpointslices", "healthy")
	other.SetNamespace("staging")
	rel := fakeRelated{"endpointslices": {recorded(t, "endpointslices", "healthy"), recorded(t, "endpointslices", "failing"), other}}
	tbl := findTable(t, p.DetailSections(svc, rel), "Endpoint Slices")
	if len(tbl.Rows) != 1 || !slices.Equal(tbl.Rows[0], []string{"web-x7k2p", "IPv4", "10.244.1.5, 10.244.2.7, 10.244.3.9", "8080/TCP"}) {
		t.Errorf("rows %q", tbl.Rows)
	}
	if l := tbl.Cells[0][0].Link; l == nil || *l != (model.ObjectRef{Kind: "endpointslices", Namespace: "shop", Name: "web-x7k2p"}) {
		t.Errorf("slice link %+v", l)
	}
	for _, s := range p.DetailSections(svc, nil) {
		if s.Title == "Endpoint Slices" {
			t.Error("a Service without related objects shows endpoint slices")
		}
	}
}

// relatedProfiles are the profiles that read related objects, with the kind of objects they read
// and the recorded object of their own kind the hostile related objects are shown with.
var relatedProfiles = []struct{ key, relatedKind, own string }{
	{"persistentvolumeclaims", "pods", "healthy"},
	{"nodes", "pods", "healthy"},
	{"nodes", "resourceslices", "healthy"},
	{"services", "endpointslices", "healthy"},
	// The workloads' drawers (DetailNeeds, D-40): their pods, a Deployment's replica sets, a Cron
	// Job's jobs.
	{"deployments", "replicasets", "healthy"},
	{"deployments", "pods", "healthy"},
	{"replicasets", "pods", "healthy"},
	{"statefulsets", "pods", "healthy"},
	{"daemonsets", "pods", "healthy"},
	{"jobs", "pods", "healthy"},
	{"cronjobs", "jobs", "healthy"},
	{"cronjobs", "pods", "healthy"},
}

// exerciseRelated runs a profile's CellsWith and DetailWith with obj as its only related object,
// the index keys included, and reports a panic.
func exerciseRelated(t *testing.T, key, relatedKind string, own, obj *unstructured.Unstructured) {
	t.Helper()
	p, _ := kinds.ByKey(key)
	rel := fakeRelated{relatedKind: {obj}}
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("%s with related %s %v: panic: %v", key, relatedKind, obj.Object, r)
		}
	}()
	for _, ix := range p.DrawerNeeds() {
		_ = ix.Keys(obj)
	}
	// Whatever its keys say, the related object reaches the cells and sections.
	all := allKeys{fakeRelated: rel}
	if p.CellsWith != nil {
		_, _ = p.CellsWith(own, all)
	}
	_ = p.DetailWith(own, all)
}

// allKeys answers every lookup with all objects of the kind, so that hostile objects reach the
// cells and sections whatever their keys say.
type allKeys struct{ fakeRelated }

func (a allKeys) Objects(ix model.RelatedIndex, _ string) []*unstructured.Unstructured {
	return a.fakeRelated[ix.Kind]
}

// TestRelatedTypeConfusion feeds every recorded object of a related kind, with every field
// replaced by each hostile value in turn, to the profiles that read that kind.
func TestRelatedTypeConfusion(t *testing.T) {
	if raceEnabled {
		t.Skip("one goroutine: the build without the race detector runs it (make test-norace-coverage)")
	}
	for _, rp := range relatedProfiles {
		own := recorded(t, rp.key, rp.own)
		for _, c := range loadCases(t, rp.relatedKind) {
			confuse(t, rp.key+" with "+rp.relatedKind+"/"+c.name, c.obj, func([]any, any) {
				exerciseRelated(t, rp.key, rp.relatedKind, own, c.obj)
			})
		}
	}
}

// FuzzRelated feeds arbitrary JSON as the related object of every profile that reads one.
func FuzzRelated(f *testing.F) {
	for _, rp := range relatedProfiles {
		for _, c := range loadCases(f, rp.relatedKind) {
			f.Add(c.json)
		}
	}
	f.Add([]byte(`{"spec":{"volumes":[{"persistentVolumeClaim":{"claimName":1}}],"nodeName":[]},"metadata":{"labels":{"kubernetes.io/service-name":null}}}`))
	own := make([]*unstructured.Unstructured, len(relatedProfiles))
	for i, rp := range relatedProfiles {
		own[i] = recorded(f, rp.key, rp.own)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		for _, decode := range []func([]byte, any) error{utiljson.Unmarshal, json.Unmarshal} {
			var m map[string]any
			if decode(data, &m) != nil || m == nil {
				continue
			}
			for i, rp := range relatedProfiles {
				exerciseRelated(t, rp.key, rp.relatedKind, own[i], &unstructured.Unstructured{Object: m})
			}
		}
	})
}
