package integration_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/helm"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

var configMapsGVR = schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}

// helmConfigMap is a release record as Helm's ConfigMaps driver writes it (HELM_DRIVER=configmap):
// a release Secret's name and labels, data.release Helm's own encoding as text.
func helmConfigMap(t *testing.T, ns, name string, rev int, status string) *unstructured.Unstructured {
	t.Helper()
	s := helmSecret(t, ns, name, rev, status)
	data, _, _ := unstructured.NestedString(s.Object, "data", "release")
	payload, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		t.Fatal(err)
	}
	cm := testkit.Object("v1", "ConfigMap", ns, s.GetName(), map[string]any{"data": map[string]any{"release": string(payload)}})
	cm.SetUID(cm.GetUID() + "-cm")
	cm.SetLabels(s.GetLabels())
	return cm
}

// TestBudgetHelmReleasesWithConfigMaps: on a cluster that serves ConfigMaps, the Releases view
// reads the release Secrets and the release ConfigMaps, one label-selected watch each and nothing
// else (no full Secrets or ConfigMaps watch); a release kept in ConfigMaps arrives over the wire
// as a ConfigMap and is decoded with its driver. The Config Maps view's full watch then serves
// the release ConfigMaps and their own watch closes, as the Secrets view's serves the release
// Secrets (covering scopes). An agent reading a release ConfigMap gets its payload redacted. For a
// server that streams initial lists and for one that does not (the kind of a listed item).
func TestBudgetHelmReleasesWithConfigMaps(t *testing.T) {
	for _, tt := range []struct {
		name      string
		watchList bool
	}{{"streaming switched on (beta)", true}, {"list and watch", false}} {
		t.Run(tt.name, func(t *testing.T) { budgetHelmReleasesWithConfigMaps(t, tt.watchList) })
	}
}

func budgetHelmReleasesWithConfigMaps(t *testing.T, watchList bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{
		DisableWatchList: !watchList,
		Resources: append(append([]testkit.FakeResource(nil), testkit.DefaultFakeResources...),
			testkit.FakeResource{GVR: testkit.SecretsGVR, Kind: "Secret", Namespaced: true},
			testkit.FakeResource{GVR: configMapsGVR, Kind: "ConfigMap", Namespaced: true}),
	})
	defer srv.Close()
	srv.Set(testkit.SecretsGVR, helmSecret(t, "apps", "web", 1, "deployed"))
	srv.Set(configMapsGVR, helmConfigMap(t, "apps", "cm", 1, "superseded"))
	srv.Set(configMapsGVR, helmConfigMap(t, "apps", "cm", 2, "deployed"))
	srv.Set(configMapsGVR, testkit.Object("v1", "ConfigMap", "apps", "settings", map[string]any{"data": map[string]any{"mode": "fast"}}))
	srv.Set(testkit.SecretsGVR, testkit.Secret("apps", "db", nil, map[string]string{"password": "hunter2"}))

	store := engine.NewClusterSettingsStore(t.TempDir())
	s := engine.New(engine.Options{
		Connector: &engine.ClusterConnector{KubeconfigPaths: []string{srv.WriteKubeconfig(t, "fake", "")}, WarmPeriod: 300 * time.Millisecond,
			ClusterSettings: store},
		ClusterSettings:  store,
		SnapshotInterval: 10 * time.Millisecond,
	})
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if watchList { // the cluster's opt-in (D-new): its reads stream their initial lists
		if _, err := s.ChangeClusterSettings("fake", func(cs *engine.ClusterSettings) error { cs.Streaming = true; return nil }); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Connect(ctx, "fake"); err != nil {
		t.Fatal(err)
	}
	open := func(view string, rows int) {
		t.Helper()
		raw, _ := json.Marshal(map[string]any{"view": view})
		if _, err := s.Dispatch(ctx, engine.Invocation{ID: "ui.openView", Params: raw, Origin: engine.OriginGUI}); err != nil {
			t.Fatal(err)
		}
		if !testkit.WaitFor(ctx, func() bool { ts := s.Table(view); return ts != nil && ts.Synced && len(ts.Rows) == rows }) {
			t.Fatalf("the %s view: %+v", view, s.Table(view))
		}
	}
	initial := func(scope string) int64 { // the initial list: streamed, or listed after the server refused the stream
		req := srv.Requests()
		if watchList {
			return req["watchlist "+scope]
		}
		return req["list "+scope]
	}

	open(helm.ReleaseKind, 2)
	cm, err := s.Object(ctx, model.ObjectRef{Kind: helm.ReleaseKind, Namespace: "apps", Name: "cm"})
	if err != nil {
		t.Fatal(err)
	}
	if helm.ReleaseStorage(cm) != helm.DriverConfigMap || helm.ReleaseRevision(cm) != 2 || model.NestedString(cm, "error") != "" {
		t.Errorf("the release kept in ConfigMaps: %v", cm.Object)
	}
	if initial("secrets with owner=helm") != 1 || initial("configmaps with owner=helm") != 1 || initial("secrets") != 0 || initial("configmaps") != 0 {
		t.Errorf("the Releases view's requests: %v", srv.Requests())
	}
	// A list-then-watch informer reports synced after its list and opens its watch after that
	// (docs/developer/testing.md › Asynchronous results): wait for the watches (CI, 2026-10-21: the
	// Secrets' was not open yet).
	if !testkit.WaitFor(ctx, func() bool {
		w := srv.OpenWatchesBy()
		return w["secrets with owner=helm"] == 1 && w["configmaps with owner=helm"] == 1
	}) {
		t.Errorf("open watches %v", srv.OpenWatchesBy())
	}

	// The Config Maps view: its full watch serves the release ConfigMaps, whose own watch closes.
	open("configmaps", 3)
	if !testkit.WaitFor(ctx, func() bool {
		w := srv.OpenWatchesBy()
		return w["configmaps"] == 1 && w["configmaps with owner=helm"] == 0
	}) {
		t.Fatalf("the release ConfigMaps did not move onto the full watch: %v", srv.OpenWatchesBy())
	}
	raw, _ := json.Marshal(map[string]any{"ref": model.ObjectRef{Kind: "configmaps", Namespace: "apps", Name: "sh.helm.release.v1.cm.v2"}})
	res, err := s.Dispatch(ctx, engine.Invocation{ID: "object.get", Params: raw, Origin: engine.OriginExternalAgent})
	if err != nil {
		t.Fatal(err)
	}
	payload, _, _ := unstructured.NestedString(helmConfigMap(t, "apps", "cm", 2, "deployed").Object, "data", "release")
	if b, _ := json.Marshal(res); strings.Contains(string(b), payload[:24]) || !strings.Contains(string(b), "release: \\u003credacted\\u003e") {
		t.Errorf("an agent reads a release ConfigMap: %s", b)
	}
	t.Logf("requests: %v", srv.Requests())
}
