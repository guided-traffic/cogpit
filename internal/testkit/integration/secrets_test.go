package integration_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/cache"
	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/helm"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// secretsServer is a fake API server that serves Secrets, and an engine connected to it whose
// cache keeps an unused informer warm for warm.
func secretsServer(ctx context.Context, t *testing.T, warm time.Duration) (*testkit.FakeAPIServer, *engine.Session) {
	t.Helper()
	srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{
		Resources: append(append([]testkit.FakeResource(nil), testkit.DefaultFakeResources...),
			testkit.FakeResource{GVR: testkit.SecretsGVR, Kind: "Secret", Namespaced: true}),
	})
	t.Cleanup(srv.Close)
	s := engine.New(engine.Options{
		Connector:        &engine.ClusterConnector{KubeconfigPaths: []string{srv.WriteKubeconfig(t, "fake", "")}, WarmPeriod: warm},
		SnapshotInterval: 10 * time.Millisecond,
	})
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if err := s.Connect(ctx, "fake"); err != nil {
		t.Fatal(err)
	}
	return srv, s
}

// secretsInitial counts the initial lists of a Secrets scope ("secrets", "secrets with
// owner=helm"): streaming watches, or lists from a server that refuses them.
func secretsInitial(srv *testkit.FakeAPIServer, resource string) int64 {
	req := srv.Requests()
	return req["watchlist "+resource] + req["list "+resource]
}

// TestBudgetSecretsViewAndDrawer runs the Secrets view of D-13 (revised: a full watch) through the
// engine on a real client-go stack against the fake API server, and counts what it costs at the
// HTTP level: the list is one full Secrets informer, values included (the last-applied annotation,
// which repeats them, is dropped as the objects enter the cache), its Keys and Type columns are
// filled, and opening a Secret's drawer costs no request at all, however often it refreshes; the
// values stay masked for agents and only the GUI can reveal one.
func TestBudgetSecretsViewAndDrawer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	srv, s := secretsServer(ctx, t, 0)
	db := testkit.Secret("apps", "db", map[string]string{"app": "db"}, map[string]string{"password": "hunter2"})
	db.SetAnnotations(map[string]string{cache.LastAppliedAnnotation: `{"apiVersion":"v1","kind":"Secret","stringData":{"password":"hunter2"}}`})
	srv.Set(testkit.SecretsGVR, db)
	srv.Set(testkit.SecretsGVR, testkit.Secret("apps", "tls", nil, map[string]string{"tls.key": "key"}))

	dispatch := func(origin engine.Origin, id string, params any) (any, error) {
		raw, _ := json.Marshal(params)
		return s.Dispatch(ctx, engine.Invocation{ID: id, Params: raw, Origin: origin})
	}
	if _, err := dispatch(engine.OriginGUI, "ui.openView", map[string]any{"view": "secrets"}); err != nil {
		t.Fatal(err)
	}
	if !testkit.WaitFor(ctx, func() bool { ts := s.Table("secrets"); return ts != nil && ts.Synced && len(ts.Rows) == 2 }) {
		t.Fatalf("no Secrets view: %+v", s.Table("secrets"))
	}
	for _, r := range s.Table("secrets").Rows {
		if r.Ref.Name == "db" && (r.Cells[3].Text != "password" || r.Cells[4].Text != "Opaque") {
			t.Errorf("Keys and Type of db: %q, %q", r.Cells[3].Text, r.Cells[4].Text)
		}
	}
	obj, err := s.Object(ctx, model.ObjectRef{Kind: "secrets", Namespace: "apps", Name: "db"})
	if err != nil {
		t.Fatal(err)
	}
	if cache.IsMetadataObject(obj) || obj.Object["data"] == nil || obj.GetAnnotations()[cache.LastAppliedAnnotation] != "" {
		t.Fatalf("the cache holds %v", obj.Object)
	}

	ref := model.ObjectRef{Context: "fake", Kind: "secrets", Namespace: "apps", Name: "db"}
	if _, err := dispatch(engine.OriginGUI, "ui.select", map[string]any{"refs": []model.ObjectRef{ref}}); err != nil {
		t.Fatal(err)
	}
	if !testkit.WaitFor(ctx, func() bool {
		m, err := s.Detail(ref)
		return err == nil && strings.Contains(sectionOf(m, "Data"), engine.MaskedValue)
	}) {
		t.Fatal("the drawer never showed the Secret's keys")
	}
	for range 5 { // the drawer refreshes: no request
		if _, err := s.Detail(ref); err != nil {
			t.Fatal(err)
		}
	}
	srv.Set(testkit.SecretsGVR, db) // a watch event on the open Secret
	time.Sleep(200 * time.Millisecond)

	res, err := dispatch(engine.OriginExternalAgent, "object.get", map[string]any{"ref": ref})
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := json.Marshal(res); strings.Contains(string(b), "hunter2") || strings.Contains(string(b), "aHVudGVyMg==") {
		t.Fatalf("object.get for an agent carries the value: %s", b)
	}
	if _, err := dispatch(engine.OriginExternalAgent, "secret.reveal", engine.RevealParams{Ref: ref, Key: "password"}); err == nil {
		t.Fatal("an agent revealed a value")
	}
	v, err := dispatch(engine.OriginGUI, "secret.reveal", engine.RevealParams{Ref: ref, Key: "password"})
	if err != nil || v.(engine.SecretValue).Value != "hunter2" {
		t.Fatalf("GUI reveal: %v, %v", v, err)
	}

	req := srv.Requests()
	if req["get secrets"] != 0 {
		t.Errorf("%d GETs of a Secret; the drawer reads the cache: %v", req["get secrets"], req)
	}
	if secretsInitial(srv, "secrets") != 1 {
		t.Errorf("want one initial request of the full Secrets: %v", req)
	}
	for k, n := range req {
		if strings.Contains(k, "secrets") && (strings.Contains(k, "(metadata)") || strings.Contains(k, " with ")) && n > 0 {
			t.Errorf("another Secrets scope was requested: %s ×%d", k, n)
		}
	}
	t.Logf("requests: %v", req)
}

// TestBudgetSecretsAndReleasesShareOneWatch: Helm's releases come from the full Secrets watch while
// it runs, and from their own owner=helm watch when it does not, so that the Releases view alone
// never holds every Secret. Measured at the HTTP level: with the Secrets view open (or warm), the
// Releases view costs no request; once the full scope's warm period ends, the Releases (still
// open) get their own owner=helm watch, the full watch closes, and the releases keep following the
// cluster; opening the Secrets view again moves them back onto its watch and closes theirs.
func TestBudgetSecretsAndReleasesShareOneWatch(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	srv, s := secretsServer(ctx, t, 300*time.Millisecond)
	for rev, status := range []string{"superseded", "deployed"} {
		srv.Set(testkit.SecretsGVR, helmSecret(t, "apps", "web", rev+1, status))
	}
	srv.Set(testkit.SecretsGVR, testkit.Secret("apps", "db", nil, map[string]string{"password": "hunter2"}))
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
	wait := func(what string, cond func() bool) {
		t.Helper()
		if !testkit.WaitFor(ctx, cond) {
			t.Fatalf("%s: requests %v, open watches %v", what, srv.Requests(), srv.OpenWatchesBy())
		}
	}
	revision := func() string {
		ts := s.Table(helm.ReleaseKind)
		if ts == nil || len(ts.Rows) != 1 {
			return ""
		}
		return ts.Rows[0].Cells[3].Text
	}

	// The Secrets view, then the Releases view: one watch, the full one.
	open("secrets", 3)
	open(helm.ReleaseKind, 1)
	if revision() != "2" {
		t.Errorf("revision %q", revision())
	}
	if n := secretsInitial(srv, "secrets with owner=helm"); n != 0 {
		t.Errorf("the Releases view sent %d owner=helm requests while the full Secrets watch runs", n)
	}
	if w := srv.OpenWatchesBy(); w["secrets"] != 1 || w["secrets with owner=helm"] != 0 {
		t.Errorf("open watches %v", w)
	}

	// The Secrets view's warm period ends with the Releases view open: their own watch takes over.
	wait("the Releases' own watch", func() bool {
		w := srv.OpenWatchesBy()
		return w["secrets"] == 0 && w["secrets with owner=helm"] == 1
	})
	if secretsInitial(srv, "secrets with owner=helm") != 1 || secretsInitial(srv, "secrets") != 1 {
		t.Errorf("after the handover: %v", srv.Requests())
	}
	if revision() != "2" {
		t.Errorf("after the handover the Releases view shows revision %q", revision())
	}
	srv.Set(testkit.SecretsGVR, helmSecret(t, "apps", "web", 2, "superseded"))
	srv.Set(testkit.SecretsGVR, helmSecret(t, "apps", "web", 3, "deployed"))
	wait("the release's new revision", func() bool { return revision() == "3" })

	// The Secrets view opens again: the (now warm) releases move onto its watch.
	open("secrets", 4)
	wait("the move back onto the full watch", func() bool {
		w := srv.OpenWatchesBy()
		return w["secrets"] == 1 && w["secrets with owner=helm"] == 0
	})
	if secretsInitial(srv, "secrets") != 2 || secretsInitial(srv, "secrets with owner=helm") != 1 {
		t.Errorf("after opening the Secrets view again: %v", srv.Requests())
	}
	t.Logf("requests: %v", srv.Requests())
}

// TestBudgetHelmReleasesView: a cluster that serves Secrets offers the Helm releases kind; its view
// and drawer are one label-selected watch on the release Secrets, with no GET and no other Secrets
// request; the releases and their history come from it.
func TestBudgetHelmReleasesView(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{
		Resources: append(append([]testkit.FakeResource(nil), testkit.DefaultFakeResources...),
			testkit.FakeResource{GVR: testkit.SecretsGVR, Kind: "Secret", Namespaced: true}),
	})
	defer srv.Close()
	for rev, status := range []string{"superseded", "deployed"} {
		srv.Set(testkit.SecretsGVR, helmSecret(t, "apps", "web", rev+1, status))
	}
	srv.Set(testkit.SecretsGVR, testkit.Secret("apps", "db", nil, map[string]string{"password": "hunter2"}))

	s := engine.New(engine.Options{
		Connector:        &engine.ClusterConnector{KubeconfigPaths: []string{srv.WriteKubeconfig(t, "fake", "")}},
		SnapshotInterval: 10 * time.Millisecond,
	})
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Connect(ctx, "fake"); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Backend().Profile("helmreleases"); !ok {
		t.Fatal("no Helm releases kind on a cluster that serves Secrets")
	}
	raw, _ := json.Marshal(map[string]any{"view": "helmreleases"})
	if _, err := s.Dispatch(ctx, engine.Invocation{ID: "ui.openView", Params: raw, Origin: engine.OriginGUI}); err != nil {
		t.Fatal(err)
	}
	if !testkit.WaitFor(ctx, func() bool { ts := s.Table("helmreleases"); return ts != nil && ts.Synced && len(ts.Rows) == 1 }) {
		t.Fatalf("no releases view: %+v", s.Table("helmreleases"))
	}
	ref := model.ObjectRef{Context: "fake", Kind: "helmreleases", Namespace: "apps", Name: "web"}
	m, err := s.DetailWait(ctx, ref)
	if err != nil || m.Missing || !strings.Contains(m.Title, "web") {
		t.Fatalf("drawer %+v, %v", m, err)
	}
	req := srv.Requests()
	if req["list secrets with owner=helm"]+req["watchlist secrets with owner=helm"] != 1 {
		t.Errorf("want one initial request for the release Secrets: %v", req)
	}
	for k, n := range req {
		if strings.Contains(k, "secrets") && !strings.Contains(k, "owner=helm") && n > 0 {
			t.Errorf("another Secrets request: %s ×%d", k, n)
		}
	}
	t.Logf("requests: %v", req)
}

// helmSecret is a release Secret as Helm's Secrets driver writes it.
func helmSecret(t *testing.T, ns, name string, rev int, status string) *unstructured.Unstructured {
	t.Helper()
	rls := map[string]any{
		"name": name, "namespace": ns, "version": rev,
		"info":  map[string]any{"status": status, "description": "revision " + strconv.Itoa(rev)},
		"chart": map[string]any{"metadata": map[string]any{"name": name, "version": "0.1.0"}},
	}
	raw, err := json.Marshal(rls)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	_, _ = zw.Write(raw)
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	payload := base64.StdEncoding.EncodeToString(buf.Bytes())
	s := testkit.Object("v1", "Secret", ns, "sh.helm.release.v1."+name+".v"+strconv.Itoa(rev), map[string]any{
		"type": "helm.sh/release.v1",
		"data": map[string]any{"release": base64.StdEncoding.EncodeToString([]byte(payload))},
	})
	s.SetLabels(map[string]string{"owner": "helm", "name": name, "version": strconv.Itoa(rev), "status": status})
	return s
}

// sectionOf joins the values of a drawer section.
func sectionOf(m *engine.DetailModel, title string) string {
	if m == nil {
		return ""
	}
	var out []string
	for _, sec := range m.Sections {
		if sec.Title == title {
			for _, it := range sec.Items {
				out = append(out, it.Value)
			}
		}
	}
	return strings.Join(out, "\n")
}
