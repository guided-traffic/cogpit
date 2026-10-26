package demo_test

import (
	"context"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	chartutil "helm.sh/helm/v4/pkg/chart/common/util"
	rspb "helm.sh/helm/v4/pkg/release/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	toolscache "k8s.io/client-go/tools/cache"
	"sigs.k8s.io/yaml"

	"github.com/guided-traffic/cogpit/internal/cache"
	"github.com/guided-traffic/cogpit/internal/demo"
	"github.com/guided-traffic/cogpit/internal/helm"
)

var (
	secretsGVR    = schema.GroupVersionResource{Version: "v1", Resource: "secrets"}
	configMapsGVR = schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}
	helmNow       = time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
)

func newHelmCluster(t *testing.T, namespaces int) *demo.Cluster {
	t.Helper()
	return demo.New(demo.Options{Seed: 1, Pods: 200, Deployments: 20, Services: 10, Events: 20, Namespaces: namespaces, NoChurn: true, Now: helmNow})
}

// records lists a resource of the cluster as the Releases view's scope reads it: labelled
// owner=helm, in all namespaces.
func records(t *testing.T, d *demo.Cluster, gvr schema.GroupVersionResource) []*unstructured.Unstructured {
	t.Helper()
	return listed(t, d, cache.Scope{GVR: gvr, LabelSelector: helm.ReleaseSelector})
}

func listed(t *testing.T, d *demo.Cluster, scope cache.Scope) []*unstructured.Unstructured {
	t.Helper()
	gvr := scope.GVR
	lw := toolscache.ToListerWatcherWithContext(d.Source().ListerWatcher(scope))
	obj, err := lw.ListWithContext(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("list %s: %v", gvr.Resource, err)
	}
	l := obj.(*unstructured.UnstructuredList)
	out := make([]*unstructured.Unstructured, len(l.Items))
	for i := range l.Items {
		out[i] = &l.Items[i]
	}
	return out
}

// decoded decodes every record as Helm's drivers do, by release and revision.
func decoded(t *testing.T, objs []*unstructured.Unstructured) map[string]*rspb.Release {
	t.Helper()
	out := map[string]*rspb.Release{}
	for _, o := range objs {
		rls, err := helm.DecodeRecord(o)
		if err != nil {
			t.Fatalf("%s %s/%s: %v", o.GetKind(), o.GetNamespace(), o.GetName(), err)
		}
		if want := "sh.helm.release.v1." + rls.Name + ".v" + strconv.Itoa(rls.Version); o.GetName() != want || rls.Namespace != o.GetNamespace() {
			t.Errorf("%s/%s holds %s/%s revision %d", o.GetNamespace(), o.GetName(), rls.Namespace, rls.Name, rls.Version)
		}
		labels := o.GetLabels()
		if labels["owner"] != "helm" || labels["name"] != rls.Name || labels["version"] != strconv.Itoa(rls.Version) ||
			labels["status"] != rls.Info.Status.String() || labels["createdAt"] == "" {
			t.Errorf("%s/%s: labels %v", o.GetNamespace(), o.GetName(), labels)
		}
		if o.GetKind() == "Secret" && o.Object["type"] != "helm.sh/release.v1" {
			t.Errorf("%s/%s: type %v", o.GetNamespace(), o.GetName(), o.Object["type"])
		}
		if o.GetUID() == "" || o.GetResourceVersion() == "" || o.GetCreationTimestamp().Time.IsZero() {
			t.Errorf("%s/%s: uid %q, resourceVersion %q, created %v", o.GetNamespace(), o.GetName(), o.GetUID(), o.GetResourceVersion(), o.GetCreationTimestamp())
		}
		checkManifest(t, rls)
		out[rls.Name+"/"+strconv.Itoa(rls.Version)] = rls
	}
	return out
}

// checkManifest: every document of a revision's manifest is one Helm wrote, "# Source:" first,
// and parses as an object; the chart has its metadata and its defaults, which the user-supplied
// values override.
func checkManifest(t *testing.T, rls *rspb.Release) {
	t.Helper()
	at := rls.Name + " revision " + strconv.Itoa(rls.Version)
	if rls.Chart == nil || rls.Chart.Metadata == nil || rls.Chart.Metadata.APIVersion != "v2" || rls.Chart.Metadata.Version == "" ||
		rls.Chart.Metadata.AppVersion == "" || len(rls.Chart.Values) == 0 || len(rls.Config) == 0 || rls.Info.Notes == "" {
		t.Fatalf("%s: chart %+v, %d values, notes %q", at, rls.Chart, len(rls.Config), rls.Info.Notes)
	}
	all, err := chartutil.CoalesceValues(rls.Chart, rls.Config)
	if err != nil || len(all) <= len(rls.Config) {
		t.Errorf("%s: all values %d keys (%v), user-supplied %d", at, len(all), err, len(rls.Config))
	}
	if strings.Contains(rls.Manifest, "<no value>") || !strings.HasPrefix(rls.Manifest, "---\n# Source: "+rls.Chart.Metadata.Name+"/templates/") {
		t.Errorf("%s: manifest\n%s", at, rls.Manifest)
	}
	for _, doc := range strings.Split(strings.TrimPrefix(rls.Manifest, "---\n"), "\n---\n") {
		var obj map[string]any
		if err := yaml.Unmarshal([]byte(doc), &obj); err != nil {
			t.Fatalf("%s: %v in\n%s", at, err, doc)
		}
		u := unstructured.Unstructured{Object: obj}
		if !strings.HasPrefix(doc, "# Source: ") || u.GetAPIVersion() == "" || u.GetKind() == "" || u.GetName() == "" ||
			u.GetLabels()["app.kubernetes.io/managed-by"] != "Helm" {
			t.Errorf("%s: document\n%s", at, doc)
		}
	}
}

// documents returns a manifest's objects by kind and name.
func documents(t *testing.T, manifest string) map[string]*unstructured.Unstructured {
	t.Helper()
	out := map[string]*unstructured.Unstructured{}
	for _, doc := range strings.Split(strings.TrimPrefix(manifest, "---\n"), "\n---\n") {
		var obj map[string]any
		if err := yaml.Unmarshal([]byte(doc), &obj); err != nil {
			t.Fatal(err)
		}
		u := &unstructured.Unstructured{Object: obj}
		out[u.GetKind()+"/"+u.GetName()] = u
	}
	return out
}

// The demo's Secrets and ConfigMaps are Helm's release records, which decode as Helm's own: the
// Releases profile derives three releases with their history from them, the last kept in
// ConfigMaps.
func TestDemoHelmReleases(t *testing.T) {
	d := newHelmCluster(t, 0)
	secrets, cms := records(t, d, secretsGVR), records(t, d, configMapsGVR)
	if len(secrets) != 8 || len(cms) != 3 {
		t.Fatalf("%d release Secrets, %d release ConfigMaps; want 8 and 3", len(secrets), len(cms))
	}
	// Nothing else: an unselected list has the same records.
	if all, allCMs := listed(t, d, cache.Scope{GVR: secretsGVR}), listed(t, d, cache.Scope{GVR: configMapsGVR}); len(all) != len(secrets) || len(allCMs) != len(cms) {
		t.Errorf("%d Secrets and %d ConfigMaps, %d and %d of them Helm's", len(all), len(allCMs), len(secrets), len(cms))
	}

	objs := helm.NewReleaseProfile().Derive(append(append([]*unstructured.Unstructured{}, secrets...), cms...))
	byName := map[string]*unstructured.Unstructured{}
	for _, o := range objs {
		byName[o.GetNamespace()+"/"+o.GetName()] = o
	}
	for key, want := range map[string]struct {
		revision int
		driver   helm.Driver
		chart    string
	}{
		"demo-shop/shop-web":     {6, helm.DriverSecret, "shop-web"},
		"demo-shop/cache":        {2, helm.DriverSecret, "redis"},
		"demo-search/search-api": {3, helm.DriverConfigMap, "search-api"},
	} {
		o := byName[key]
		if o == nil {
			t.Fatalf("no release %s in %d", key, len(objs))
		}
		chart, _ := helm.ReleaseChart(o)
		if helm.ReleaseRevision(o) != want.revision || helm.ReleaseStatus(o) != "deployed" || helm.ReleaseStorage(o) != want.driver ||
			chart != want.chart || len(helm.ReleaseHistory(o)) != want.revision {
			t.Errorf("%s: revision %d %s, %s, chart %s, %d in its history", key, helm.ReleaseRevision(o), helm.ReleaseStatus(o),
				helm.ReleaseStorage(o), chart, len(helm.ReleaseHistory(o)))
		}
	}
	if len(objs) != 3 {
		t.Errorf("%d releases, want 3", len(objs))
	}

	hist := helm.ReleaseHistory(byName["demo-shop/shop-web"]) // newest first
	wantHist := []struct {
		status, chartVersion, description string
	}{
		{"deployed", "1.5.0", "Rollback to 4"},
		{"failed", "1.5.0", `Upgrade "shop-web" failed: client rate limiter Wait returned an error: context deadline exceeded`},
		{"superseded", "1.5.0", "Upgrade complete"},
		{"superseded", "1.4.0", "Upgrade complete"},
		{"superseded", "1.4.0", "Upgrade complete"},
		{"superseded", "1.4.0", "Install complete"},
	}
	for i, w := range wantHist {
		h := hist[i]
		if h.Revision != 6-i || h.Status != w.status || h.ChartVersion != w.chartVersion || h.Description != w.description || h.Updated.IsZero() {
			t.Errorf("shop-web history %d: %+v, want %+v", i, h, w)
		}
		if i > 0 && !h.Updated.Before(hist[i-1].Updated) {
			t.Errorf("shop-web revision %d updated %v, not before revision %d's %v", h.Revision, h.Updated, hist[i-1].Revision, hist[i-1].Updated)
		}
	}
}

// What changes between shop-web's revisions shows in their values and manifests: replicas and a
// memory limit, an image tag, a chart version that adds a ConfigMap and drops the metrics Service
// and changes the database password, a failed upgrade, and the rollback that restores revision 4.
func TestDemoHelmRevisionsDiffer(t *testing.T) {
	d := newHelmCluster(t, 0)
	revs := decoded(t, append(records(t, d, secretsGVR), records(t, d, configMapsGVR)...))
	if len(revs) != 11 {
		t.Fatalf("%d revisions, want 11", len(revs))
	}
	rev := func(n int) (*rspb.Release, map[string]*unstructured.Unstructured) {
		r := revs["shop-web/"+strconv.Itoa(n)]
		if r == nil {
			t.Fatalf("no shop-web revision %d", n)
		}
		return r, documents(t, r.Manifest)
	}
	replicas := func(docs map[string]*unstructured.Unstructured) int64 {
		n, _, _ := unstructured.NestedFieldNoCopy(docs["Deployment/shop-web"].Object, "spec", "replicas")
		f, _ := n.(float64) // YAML's numbers decode as JSON's
		return int64(f)
	}
	image := func(docs map[string]*unstructured.Unstructured) string {
		cs, _, _ := unstructured.NestedSlice(docs["Deployment/shop-web"].Object, "spec", "template", "spec", "containers")
		img, _, _ := unstructured.NestedString(cs[0].(map[string]any), "image")
		return img
	}
	password := func(docs map[string]*unstructured.Unstructured) string {
		p, _, _ := unstructured.NestedString(docs["Secret/shop-web-db"].Object, "data", "password")
		return p
	}

	r1, d1 := rev(1)
	r2, d2 := rev(2)
	_, d3 := rev(3)
	r4, d4 := rev(4)
	r5, d5 := rev(5)
	r6, d6 := rev(6)
	if replicas(d1) != 2 || replicas(d2) != 3 || reflect.DeepEqual(r1.Config["resources"], r2.Config["resources"]) {
		t.Errorf("revision 2: replicas %d → %d, resources %v → %v", replicas(d1), replicas(d2), r1.Config["resources"], r2.Config["resources"])
	}
	if image(d2) != "registry.example.com/shop/web:2.3.0" || image(d3) != "registry.example.com/shop/web:2.3.1" {
		t.Errorf("revision 3: image %s → %s", image(d2), image(d3))
	}
	if d3["Service/shop-web-metrics"] == nil || d3["ConfigMap/shop-web-config"] != nil ||
		d4["Service/shop-web-metrics"] != nil || d4["ConfigMap/shop-web-config"] == nil ||
		password(d3) == password(d4) || r4.Chart.Metadata.Version != "1.5.0" || r4.Chart.Metadata.AppVersion != "2.4.0" {
		t.Errorf("revision 4: chart %s, documents %v → %v", r4.Chart.Metadata.Version, keys(d3), keys(d4))
	}
	if replicas(d5) != 5 || len(d5) != len(d4) || reflect.DeepEqual(r5.Config["ingress"], r4.Config["ingress"]) || r5.Info.Status != "failed" {
		t.Errorf("revision 5: replicas %d, status %s, ingress %v", replicas(d5), r5.Info.Status, r5.Config["ingress"])
	}
	if r6.Manifest != r4.Manifest || !reflect.DeepEqual(r6.Config, r4.Config) || r6.Info.Notes != r4.Info.Notes ||
		r6.Info.RollbackRevision != 4 || r6.Chart.Metadata.Version != "1.5.0" || len(d6) != len(d4) {
		t.Errorf("revision 6 is not revision 4 rolled back: rollback %d, chart %s", r6.Info.RollbackRevision, r6.Chart.Metadata.Version)
	}
	if !r6.Info.FirstDeployed.Equal(r1.Info.LastDeployed) || !r6.Info.LastDeployed.After(r5.Info.LastDeployed) {
		t.Errorf("revision 6: first deployed %v, last %v", r6.Info.FirstDeployed, r6.Info.LastDeployed)
	}
	// The user-supplied database password is the one the Secret holds.
	if db, _ := r4.Config["database"].(map[string]any); db == nil || password(d4) == "" {
		t.Errorf("revision 4: database values %v", r4.Config["database"])
	}

	// cache: a patch version of its chart, the same values.
	c1, c2 := revs["cache/1"], revs["cache/2"]
	if c1 == nil || c2 == nil || c1.Chart.Metadata.Version != "20.6.1" || c2.Chart.Metadata.Version != "20.6.2" ||
		!reflect.DeepEqual(c1.Config, c2.Config) || c1.Manifest == c2.Manifest {
		t.Errorf("cache revisions %v, %v", c1, c2)
	}
	// search-api: kept in ConfigMaps; 0.10.0 adds a PodDisruptionBudget.
	if s3 := revs["search-api/3"]; s3 == nil || documents(t, s3.Manifest)["PodDisruptionBudget/search-api"] == nil ||
		documents(t, revs["search-api/2"].Manifest)["PodDisruptionBudget/search-api"] != nil {
		t.Errorf("search-api revision 3: %v", s3)
	}
}

func keys(m map[string]*unstructured.Unstructured) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// A demo cluster without a release's namespace, or with it being deleted, has no records of that
// release: with three namespaces none; with six the last, demo-search, is Terminating, so
// search-api is left out.
func TestDemoHelmReleasesFollowTheNamespaces(t *testing.T) {
	if s, c := records(t, newHelmCluster(t, 3), secretsGVR), records(t, newHelmCluster(t, 3), configMapsGVR); len(s)+len(c) != 0 {
		t.Errorf("three namespaces: %d Secrets, %d ConfigMaps", len(s), len(c))
	}
	d := newHelmCluster(t, 6)
	if s, c := records(t, d, secretsGVR), records(t, d, configMapsGVR); len(s) != 8 || len(c) != 0 {
		t.Errorf("six namespaces: %d Secrets, %d ConfigMaps; want 8 and 0", len(s), len(c))
	}
}
