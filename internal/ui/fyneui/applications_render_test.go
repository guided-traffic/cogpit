package fyneui

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
)

// The Applications view and the Resource Claims view as their real profiles fill them, each with
// its drawer open, and the sidebar with Applications at the top and the Device Resources group.
// PNGs: testdata/applications_view_{dark,light}.png, testdata/resourceclaims_view_{dark,light}.png.

// appWorkloadSpec is one fixture workload of an application.
type appWorkloadSpec struct {
	ns, instance, kind, name, version, managedBy string
	status                                       model.Status
	age                                          time.Duration
}

func fixtureAppWorkloads() []kinds.AppWorkload {
	day := 24 * time.Hour
	specs := []appWorkloadSpec{
		{"storefront", "storefront", "deployments", "web", "1.4.2", "Helm", model.StatusOK, 40 * day},
		{"storefront", "storefront", "deployments", "gateway", "1.4.2", "Helm", model.StatusWarning, 40 * day},
		{"storefront", "storefront", "statefulsets", "cart-db", "1.4.2", "Helm", model.StatusOK, 40 * day},
		{"storefront", "storefront", "cronjobs", "sitemap", "1.4.2", "Helm", model.StatusOK, 12 * day},
		{"payments", "billing", "deployments", "billing-api", "3.1.0", "Helm", model.StatusOK, 90 * day},
		{"payments", "billing", "jobs", "migrate-v3", "3.1.0", "Helm", model.StatusError, 2 * time.Hour},
		{"search", "search", "statefulsets", "opensearch", "2.19.1", "kustomize", model.StatusOK, 160 * day},
		{"search", "search", "deployments", "indexer", "2.19.1", "kustomize", model.StatusPending, 5 * time.Minute},
		{"identity", "auth", "deployments", "auth-gateway", "8.2.0", "argocd", model.StatusOK, 70 * day},
		{"identity", "auth", "deployments", "token-service", "8.2.0", "argocd", model.StatusOK, 70 * day},
		{"monitoring", "metrics", "daemonsets", "node-exporter", "1.9.1", "Helm", model.StatusOK, 160 * day},
		{"monitoring", "metrics", "statefulsets", "prometheus", "3.5.0", "Helm", model.StatusOK, 160 * day},
		{"qa", "image-resizer", "deployments", "image-resizer", "0.3.0", "kustomize", model.StatusNone, 9 * day},
		{"orders", "ledger", "deployments", "ledger-writer", "5.0.0", "Helm", model.StatusOK, 30 * day},
		{"orders", "ledger", "cronjobs", "ledger-compaction", "5.0.0", "Helm", model.StatusOK, 30 * day},
	}
	out := make([]kinds.AppWorkload, 0, len(specs))
	for _, s := range specs {
		obj := &unstructured.Unstructured{Object: map[string]any{
			"metadata": map[string]any{
				"name": s.name, "namespace": s.ns, "uid": "uid-" + s.ns + "-" + s.name, "creationTimestamp": ts(s.age),
				"labels": map[string]any{
					kinds.LabelAppInstance: s.instance, kinds.LabelAppName: s.instance, kinds.LabelAppVersion: s.version,
					kinds.LabelAppManagedBy: s.managedBy,
				},
			},
		}}
		out = append(out, kinds.AppWorkload{Kind: s.kind, Object: obj, Status: s.status})
	}
	return out
}

// dataKinds are the kinds the sidebar shows besides the fixture's: Applications at the top, the
// workload kinds its links lead to, and the Device Resources group.
var dataKinds = []kindInfo{
	{Kind: kinds.ApplicationsKey, Title: "Applications", APIKind: "Application", Group: model.LocalGroup, Version: "v1", Namespaced: true, Virtual: true},
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
	apps := kinds.Applications()
	profileSnapshot(t, f, apps, kinds.GroupApplications(fixtureAppWorkloads()))
	claims, _ := kinds.ByKey("resourceclaims")
	profileSnapshot(t, f, claims, fixtureClaimObjects())
	run := func(context.Context, *engine.Session, probeRef) (any, error) { return nil, nil }
	if err := engine.Register(f.reg, engine.Spec[probeRef]{ID: "application.logs", Label: "Logs", Kinds: []string{kinds.ApplicationsKey},
		Order: 300, Danger: engine.DangerRead, Run: run}); err != nil {
		t.Fatal(err)
	}
	if view == kinds.ApplicationsKey {
		// As the engine: Edit and Delete do not apply to a virtual kind's objects.
		f.disabled["object.delete"] = notApplicable
	}
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

var (
	storefrontApp = model.ObjectRef{Context: "demo", Kind: kinds.ApplicationsKey, Namespace: "storefront", Name: "storefront"}
	inferenceGPU  = model.ObjectRef{Context: "demo", Kind: "resourceclaims", Namespace: "ml", Name: "inference-gpu"}
)

func TestRenderApplicationsViewDark(t *testing.T) {
	renderDataView(t, kinds.ApplicationsKey, storefrontApp, ThemeDark, "applications_view_dark.png")
}

func TestRenderResourceClaimsViewDark(t *testing.T) {
	renderDataView(t, "resourceclaims", inferenceGPU, ThemeDark, "resourceclaims_view_dark.png")
}

// TestSidebarApplicationsAndDeviceResources: Applications follows the cluster Overview, as Lens's
// sidebar has it, and the Device Resources group holds its four views between Storage and
// Namespaces.
func TestSidebarApplicationsAndDeviceResources(t *testing.T) {
	f := dataFake(t, kinds.ApplicationsKey, storefrontApp)
	sh, _, q := newTestShell(t, f, Options{})
	q.until(t, "the sidebar", func() bool { return len(sh.kinds) > 0 })
	roots := sh.side.children[""]
	if len(roots) < 2 || roots[0] != engine.ClusterOverview || roots[1] != kinds.ApplicationsKey {
		t.Errorf("sidebar roots %v: the Overview, then Applications", roots)
	}
	devices := sh.side.children[groupPrefix+"devices"]
	if strings.Join(devices, ",") != "resourceclaims,resourceclaimtemplates,deviceclasses,resourceslices" {
		t.Errorf("Device Resources %v", devices)
	}
	if i, j := slices.Index(roots, groupPrefix+"devices"), slices.Index(roots, kindNamespaces); i < 0 || j != i+1 {
		t.Errorf("Device Resources at %d, Namespaces at %d: %v", i, j, roots)
	}
	if other := sh.side.children[otherGroup]; slices.Contains(other, kinds.ApplicationsKey) {
		t.Error("Applications under Other")
	}
}
