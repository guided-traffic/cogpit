package fyneui

import (
	"slices"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
)

// The Resource Claims view as its real profile fills it, its drawer open, and the sidebar with the
// Device Resources group. PNG: testdata/resourceclaims_view_dark.png.

// dataKinds are the kinds the sidebar shows besides the fixture's: the other workload kinds and the
// Device Resources group.
var dataKinds = []kindInfo{
	{Kind: "statefulsets", Title: "Stateful Sets", APIKind: "StatefulSet", Group: "apps", Version: "v1", Namespaced: true},
	{Kind: "daemonsets", Title: "Daemon Sets", APIKind: "DaemonSet", Group: "apps", Version: "v1", Namespaced: true},
	{Kind: "jobs", Title: "Jobs", APIKind: "Job", Group: "batch", Version: "v1", Namespaced: true},
	{Kind: "cronjobs", Title: "Cron Jobs", APIKind: "CronJob", Group: "batch", Version: "v1", Namespaced: true},
	{Kind: "resourceclaims", Title: "Resource Claims", APIKind: "ResourceClaim", Group: "resource.k8s.io", Version: "v1", Namespaced: true},
	{Kind: "resourceclaimtemplates", Title: "Resource Claim Templates", APIKind: "ResourceClaimTemplate", Group: "resource.k8s.io", Version: "v1", Namespaced: true},
	{Kind: "deviceclasses", Title: "Device Classes", APIKind: "DeviceClass", Group: "resource.k8s.io", Version: "v1"},
	{Kind: "resourceslices", Title: "Resource Slices", APIKind: "ResourceSlice", Group: "resource.k8s.io", Version: "v1"},
}

// profileSnapshot builds a view's snapshot and every object's drawer with a profile, as the engine
// does.
func profileSnapshot(t testing.TB, f *fakeEngine, p *model.KindProfile, objs []*unstructured.Unstructured) {
	t.Helper()
	rows := make([]model.Row, 0, len(objs))
	for _, o := range objs {
		rows = append(rows, model.BuildRow(p, "demo", o))
		ref := model.ObjectRef{Context: "demo", Kind: p.Key, Namespace: o.GetNamespace(), Name: o.GetName()}
		m, err := engine.BuildDetailWith(p, ref, o, nil, fixedNow, nil)
		if err != nil {
			t.Fatal(err)
		}
		f.details[ref.String()] = m
	}
	slices.SortFunc(rows, func(a, b model.Row) int { return strings.Compare(a.Ref.Name, b.Ref.Name) })
	f.tables[p.Key] = &engine.TableSnapshot{
		View: p.Key, Context: "demo", Title: p.Title, Namespaced: p.Namespaced, Columns: p.Columns, Rows: rows,
		Sort: engine.SortSpec{Column: "name"}, Total: len(rows), Revision: 1, Synced: true,
	}
}

// claimFixture is one fixture resource claim: its device class, allocated devices, consumers,
// and a device a driver reports unhealthy.
type claimFixture struct {
	ns, name, class string
	devices         []string
	pods            []string
	unhealthy       string
	age             time.Duration
}

func fixtureClaimObjects() []*unstructured.Unstructured {
	day := 24 * time.Hour
	specs := []claimFixture{
		{"ml", "trainer-0-gpu-x2v9k", "gpu.example.com", []string{"gpu-0"}, []string{"trainer-0"}, "", 2 * day},
		{"ml", "trainer-1-gpu-q8m4t", "gpu.example.com", []string{"gpu-1"}, []string{"trainer-1"}, "", 2 * day},
		{"ml", "inference-gpu", "gpu.example.com", []string{"gpu-3"}, []string{"inference-7d9f-abcde"}, "gpu-3", 3 * time.Hour},
		{"ml", "shared-accel", "gpu.example.com", []string{"gpu-4", "gpu-5", "gpu-6", "gpu-7"}, []string{"worker-0", "worker-1"}, "", 9 * day},
		{"media", "render-devices", "gpu-large.example.com", nil, nil, "", 5 * time.Minute},
		{"net", "fast-nic", "nic.example.net", []string{"vf-7"}, nil, "", 30 * day},
		{"batch", "nightly-nic", "nic.example.net", nil, nil, "", 40 * time.Minute},
	}
	out := make([]*unstructured.Unstructured, 0, len(specs))
	for _, s := range specs {
		status := map[string]any{}
		if len(s.devices) > 0 {
			var results, health []any
			for _, d := range s.devices {
				results = append(results, map[string]any{"request": "gpu", "driver": s.class, "pool": "worker-a-01", "device": d})
				cond := map[string]any{"type": "Ready", "status": "True"}
				if d == s.unhealthy {
					cond = map[string]any{"type": "Ready", "status": "False", "reason": "XidError", "message": "Xid 79: GPU has fallen off the bus"}
				}
				health = append(health, map[string]any{"driver": s.class, "pool": "worker-a-01", "device": d, "conditions": []any{cond}})
			}
			status["allocation"] = map[string]any{"devices": map[string]any{"results": results}}
			status["devices"] = health
		}
		var consumers []any
		for _, p := range s.pods {
			consumers = append(consumers, map[string]any{"resource": "pods", "name": p, "uid": "uid-" + p})
		}
		if len(consumers) > 0 {
			status["reservedFor"] = consumers
		}
		out = append(out, &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "resource.k8s.io/v1", "kind": "ResourceClaim",
			"metadata": map[string]any{"name": s.name, "namespace": s.ns, "uid": "uid-claim-" + s.name, "creationTimestamp": ts(s.age)},
			"spec": map[string]any{"devices": map[string]any{"requests": []any{map[string]any{
				"name": "gpu", "exactly": map[string]any{"deviceClassName": s.class},
			}}}},
			"status": status,
		}})
	}
	return out
}

func dataFake(t testing.TB, view string, drawer model.ObjectRef) *fakeEngine {
	f := connectedFake(t, 1)
	f.kinds = append(f.kinds, dataKinds...)
	claims, _ := kinds.ByKey("resourceclaims")
	profileSnapshot(t, f, claims, fixtureClaimObjects())
	f.ui.View = view
	f.ui.Selected, f.ui.Drawer = []model.ObjectRef{drawer}, &drawer
	return f
}

func renderDataView(t *testing.T, view string, drawer model.ObjectRef, mode ThemeMode, name string) {
	f := dataFake(t, view, drawer)
	sh, w, q := newTestShell(t, f, Options{Theme: mode})
	q.until(t, "the "+view+" view and its drawer", func() bool {
		return len(sh.kinds) > 0 && sh.view.snap != nil && sh.view.snap.View == view && sh.drawer.last != nil && sh.diag.total > 0
	})
	settle(w)
	savePNG(t, w, name)
}

var inferenceGPU = model.ObjectRef{Context: "demo", Kind: "resourceclaims", Namespace: "ml", Name: "inference-gpu"}

func TestRenderResourceClaimsViewDark(t *testing.T) {
	renderDataView(t, "resourceclaims", inferenceGPU, ThemeDark, "resourceclaims_view_dark.png")
}

// TestSidebarDeviceResources: the Device Resources group holds its four views between Storage and
// Namespaces.
func TestSidebarDeviceResources(t *testing.T) {
	f := dataFake(t, "resourceclaims", inferenceGPU)
	sh, _, q := newTestShell(t, f, Options{})
	q.until(t, "the sidebar", func() bool { return len(sh.kinds) > 0 })
	roots := sh.side.children[""]
	devices := sh.side.children[groupPrefix+"devices"]
	if strings.Join(devices, ",") != "resourceclaims,resourceclaimtemplates,deviceclasses,resourceslices" {
		t.Errorf("Device Resources %v", devices)
	}
	if i, j := slices.Index(roots, groupPrefix+"devices"), slices.Index(roots, kindNamespaces); i < 0 || j != i+1 {
		t.Errorf("Device Resources at %d, Namespaces at %d: %v", i, j, roots)
	}
}
