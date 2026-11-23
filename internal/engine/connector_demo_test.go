//go:build !nodemo

// The demo cluster's tests: a development build only, as connector_demo.go (D-67).

package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/guided-traffic/cogpit/internal/cache"
	"github.com/guided-traffic/cogpit/internal/demo"
	"github.com/guided-traffic/cogpit/internal/helm"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
)

// TestDemoProfilesFollowTheDemo checks that the demo cluster offers exactly the built-in kinds it
// serves, as discovery decides for a real cluster: a profile the demo has no objects for (Ingresses,
// Stateful Sets) must not become a view that only shows a 404.
func TestDemoProfilesFollowTheDemo(t *testing.T) {
	served := demo.Resources()
	offered := demoProfiles()
	if len(offered) == 0 {
		t.Fatal("the demo offers no kinds")
	}
	for _, p := range kinds.Builtin() {
		if want, got := slices.Contains(served, p.GVR()), slices.Contains(offered, p); got != want {
			t.Errorf("%s: offered %v, served by the demo %v", p.Key, got, want)
		}
	}
}

func TestClusterConnectorContextsAndDemo(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no-such-kubeconfig")
	c := &ClusterConnector{KubeconfigPaths: []string{missing}, IncludeDemo: true, DemoOptions: demo.Options{Pods: 20, Services: 3, Namespaces: 2, Nodes: 2, NoChurn: true}}
	list, err := c.Contexts()
	if len(list) == 0 || !list[len(list)-1].Demo || list[len(list)-1].Name != DemoContext {
		t.Errorf("demo entry missing: %+v (err %v)", list, err)
	}
	if _, statErr := os.Stat(missing); statErr == nil {
		t.Error("the connector created a kubeconfig file")
	}
	if !IsDemoContext(DemoContext) {
		t.Error("a development build does not know its demo cluster's context")
	}

	b, err := c.Connect(context.Background(), DemoContext)
	if errors.Is(err, errDemoUnavailable) {
		t.Skip("the demo package is a stub in this tree")
	}
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if !b.Demo || b.Context != DemoContext || b.Conn != nil || b.CanWrite() || len(b.Profiles) == 0 || b.DemoStats == nil {
		t.Errorf("demo backend %+v", b)
	}
	// Its API server's version, the one its kube-apiserver pods name (D-91).
	if b.ServerVersion() != demo.ServerVersion || demo.ServerVersion == "" {
		t.Errorf("the demo's API server version %q, want %q", b.ServerVersion(), demo.ServerVersion)
	}
	// Only the built-in kinds the demo serves get a view, as discovery decides on a real cluster, then
	// Helm's releases from its Secrets and ConfigMaps and its custom kinds from their definitions.
	builtin := 0
	for _, p := range kinds.Builtin() {
		if slices.Contains(demo.Resources(), p.GVR()) {
			builtin++
		}
	}
	custom := len(demo.CustomResourceDefinitions())
	if len(b.Profiles) != builtin+1+custom || !b.CustomResources {
		t.Errorf("demo offers %d views for %d served built-in kinds, Releases and %d custom kinds (custom kinds %v)", len(b.Profiles), builtin, custom, b.CustomResources)
	}
}

// TestDemoVeleroPage: the demo cluster's Velero shows on the Velero page as its file describes it
// (demo/velero.go): nightly-all overdue while its last backup runs, shop-volumes partially failed,
// monitoring-config paused, the secondary location unavailable, demo-shop's repository's
// maintenance failed, demo-ml outside every schedule with its PVCs first.
func TestDemoVeleroPage(t *testing.T) {
	c := &ClusterConnector{KubeconfigPaths: []string{filepath.Join(t.TempDir(), "none")}, IncludeDemo: true,
		DemoOptions: demo.Options{Pods: 50, Deployments: 10, Services: 3, Namespaces: 20, Nodes: 3, Events: 10, NoChurn: true}}
	s := New(Options{Connector: c, SnapshotInterval: 5 * time.Millisecond})
	t.Cleanup(s.Close)
	if err := s.Connect(context.Background(), DemoContext); err != nil {
		if errors.Is(err, errDemoUnavailable) {
			t.Skip("the demo package is a stub in this tree")
		}
		t.Fatal(err)
	}
	waitFor(t, "Velero's kinds", func() bool { return hasKind(s, VeleroBackupsKey) })
	m, err := s.VeleroDashboard(context.Background(), "", nil)
	if err != nil || !m.Served || !m.Synced {
		t.Fatalf("%+v %v", m, err)
	}
	states := map[string]string{}
	for _, r := range m.Schedules {
		states[r.Ref.Name] = r.State
	}
	if states["nightly-all"] != lblOverdue || states["shop-volumes"] != lblPartiallyFailed || states["monitoring-config"] != lblPaused ||
		states["hourly-db"] != lblEnabled || states["weekly-full"] != lblEnabled {
		t.Errorf("schedules %v", states)
	}
	var problems []string
	for _, a := range m.Attention {
		problems = append(problems, a.Problem+" "+a.Object)
	}
	for _, want := range []string{"Location unavailable BackupStorageLocation secondary", "Overdue Schedule nightly-all",
		"Maintenance failed BackupRepository demo-shop-default-kopia"} {
		if !slices.Contains(problems, want) {
			t.Errorf("attention %v: no %q", problems, want)
		}
	}
	if len(m.Outside) == 0 || m.Outside[0].Ref.Name != "demo-ml" || m.Outside[0].PVCs != 4 {
		t.Errorf("outside %+v (%s) tiles %s", m.Outside, m.OutsideError, tileText(m))
	}
	if h := m.Header; h.Version != "1.17.1" || h.Namespace != "velero" || h.NodeAgents == nil || h.NodeAgents.Desired != 3 ||
		len(h.Plugins) != 1 || h.Plugins[0].Name != "aws" {
		t.Errorf("header %+v", h)
	}
	// shop-volumes' four errors are its four failed pod volume backups, in two groups.
	var shop string
	for _, a := range m.Attention {
		if strings.HasPrefix(a.Object, "Backup shop-volumes-") {
			shop = a.Detail
		}
	}
	if shop != "4 errors, 12 warnings · 4 volume backups failed · 2 of 3 volume snapshots" {
		t.Errorf("shop-volumes' detail %q", shop)
	}
	if len(m.FailedVolumes) != 2 || m.FailedVolumes[0].Volumes != 3 || m.FailedVolumes[0].Pods != 3 || m.FailedVolumes[0].Namespace != "demo-shop" ||
		m.FailedVolumes[1].Volumes != 1 || !strings.HasPrefix(m.FailedVolumes[1].Message, "exposed PVB is not ready") ||
		m.FailedVolumes[0].Ref.Kind != VeleroPodVolumeBackupsKey {
		t.Errorf("failed volumes %+v", m.FailedVolumes)
	}
}

// TestDemoVeleroStatesAllDay: the demo's schedules read the same at every time of day, as its file
// describes them: nightly-all overdue, the others not (their last run lies less than the page's
// grace back, demo/velero.go).
func TestDemoVeleroStatesAllDay(t *testing.T) {
	base := time.Date(2026, 11, 4, 0, 0, 0, 0, time.UTC)
	for off := time.Duration(0); off < 26*time.Hour; off += 7 * time.Minute {
		now := base.Add(off)
		d := demo.New(demo.Options{Pods: 20, Deployments: 5, Services: 2, Namespaces: 20, Nodes: 3, Events: 5, NoChurn: true, Now: now})
		objs := map[string][]*unstructured.Unstructured{}
		for key, gvr := range map[string]schema.GroupVersionResource{
			VeleroSchedulesKey: {Group: "velero.io", Version: "v1", Resource: "schedules"},
			VeleroBackupsKey:   {Group: "velero.io", Version: "v1", Resource: "backups"},
			VeleroLocationsKey: {Group: "velero.io", Version: "v1", Resource: "backupstoragelocations"},
		} {
			lw, ok := d.Source().ListerWatcher(cache.Scope{GVR: gvr}).(interface {
				ListWithContext(context.Context, metav1.ListOptions) (runtime.Object, error)
			})
			if !ok {
				t.Fatal("the demo's lister lists without a context")
			}
			list, err := lw.ListWithContext(context.Background(), metav1.ListOptions{})
			if err != nil {
				t.Fatal(err)
			}
			for i := range list.(*unstructured.UnstructuredList).Items {
				objs[key] = append(objs[key], &list.(*unstructured.UnstructuredList).Items[i])
			}
		}
		vb := &veleroBuild{served: true, synced: true, objs: objs, read: map[string]bool{}, kindOK: map[string]bool{}, kindErr: map[string]string{}}
		for _, k := range veleroPageKinds {
			vb.read[k], vb.kindOK[k] = true, true
		}
		m := buildVeleroModel(&Backend{Context: DemoContext}, vb, nil, now.Add(30*time.Second), model.TimeFormat{})
		for _, r := range m.Schedules {
			if overdue := r.State == lblOverdue; overdue != (r.Ref.Name == "nightly-all") {
				t.Fatalf("at %s: %s is %s", now.Format("15:04"), r.Ref.Name, r.State)
			}
		}
	}
}

// TestDemoOffersHelmReleases: the demo cluster serves Secrets, so it offers Helm's releases, once,
// with a memo of its own per call (one per backend).
func TestDemoOffersHelmReleases(t *testing.T) {
	var found []*model.KindProfile
	for _, p := range demoProfiles() {
		if isHelmReleases(p) {
			found = append(found, p)
		}
	}
	if len(found) != 1 {
		t.Fatalf("the demo cluster offers Releases %d times", len(found))
	}
	for _, p := range demoProfiles() {
		if isHelmReleases(p) && p == found[0] {
			t.Error("two demo backends share one releases profile and its memo")
		}
	}
}

// TestDemoHelmReleases: the demo's Releases view lists its three releases, the one kept in
// ConfigMaps joined with those kept in Secrets as for a real cluster (Backend.Acquire), each at its
// newest revision.
func TestDemoHelmReleases(t *testing.T) {
	c := &ClusterConnector{KubeconfigPaths: []string{filepath.Join(t.TempDir(), "none")}, IncludeDemo: true,
		DemoOptions: demo.Options{Pods: 50, Deployments: 10, Services: 3, Namespaces: 8, Nodes: 3, Events: 10, NoChurn: true}}
	s := New(Options{Connector: c, SnapshotInterval: 5 * time.Millisecond})
	t.Cleanup(s.Close)
	if err := s.Connect(context.Background(), DemoContext); err != nil {
		if errors.Is(err, errDemoUnavailable) {
			t.Skip("the demo package is a stub in this tree")
		}
		t.Fatal(err)
	}
	p, ok := s.Backend().Profile(helm.ReleaseKind)
	if !ok {
		t.Fatal("the demo backend has no Releases")
	}
	openView(s, helm.ReleaseKind)
	snap := waitTable(t, s, helm.ReleaseKind, func(t *TableSnapshot) bool { return t.Synced && len(t.Rows) == 3 })
	want := map[string]struct {
		revision int64
		driver   helm.Driver
	}{
		"demo-shop/shop-web": {6, helm.DriverSecret}, "demo-shop/cache": {2, helm.DriverSecret},
		"demo-search/search-api": {3, helm.DriverConfigMap},
	}
	for _, r := range snap.Rows {
		w, ok := want[r.Ref.Namespace+"/"+r.Ref.Name]
		obj, err := s.Lookup(r.Ref)
		if !ok || err != nil || r.Cells[p.ColumnIndex("revision")].Num != w.revision ||
			r.Cells[p.ColumnIndex("status")].Text != "Deployed" || helm.ReleaseStorage(obj) != w.driver {
			t.Errorf("row %s: %+v (lookup %v)", r.Ref.Key(), r.Cells, err)
		}
	}
}
