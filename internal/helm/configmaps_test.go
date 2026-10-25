package helm

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"helm.sh/helm/v4/pkg/release/common"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"

	"github.com/guided-traffic/cogpit/internal/model"
)

// configMapFor encodes a revision the way Helm's ConfigMaps driver stores it (HELM_DRIVER=
// configmap): the record's name and labels as for a Secret, data.release Helm's own encoding as
// text, without a Secret's Kubernetes base64.
func configMapFor(t *testing.T, r testRelease) *unstructured.Unstructured {
	t.Helper()
	s := secretFor(t, r)
	payload, err := DecodeSecret(s) // the release as secretFor built it
	if err != nil {
		t.Fatal(err)
	}
	payload.Labels = nil
	cm := s.DeepCopy()
	cm.SetKind("ConfigMap")
	delete(cm.Object, "type")
	cm.SetUID(types.UID("uid-cm-" + r.ns + "-" + cm.GetName()))
	cm.Object["data"] = map[string]any{"release": encode(t, payload, true)}
	return cm
}

// A release kept in ConfigMaps is listed, read and shown like one kept in Secrets: the same
// columns, history, values (private), notes and resources; its object names its driver and the
// ConfigMap of its newest revision, and the drawer's Storage says ConfigMaps.
func TestDeriveFromConfigMaps(t *testing.T) {
	p := NewReleaseProfile()
	cms := []*unstructured.Unstructured{
		configMapFor(t, testRelease{ns: "apps", name: "web", rev: 1, status: common.StatusSuperseded}),
		configMapFor(t, testRelease{ns: "apps", name: "web", rev: 2, status: common.StatusDeployed, notes: "Password: hunter2",
			values: map[string]any{"password": "hunter2"}, manifest: "---\napiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: web\n"}),
	}
	objs := p.Derive(cms)
	if len(objs) != 1 {
		t.Fatalf("%d releases", len(objs))
	}
	web := objs[0]
	if ReleaseRevision(web) != 2 || ReleaseStatus(web) != "deployed" || string(web.GetUID()) != "uid-cm-apps-sh.helm.release.v1.web.v2" ||
		model.NestedString(web, "error") != "" || len(ReleaseRevisions(web)) != 2 {
		t.Errorf("release %v", web.Object)
	}
	if d, name := ReleaseRecord(web); d != DriverConfigMap || name != "sh.helm.release.v1.web.v2" || ReleaseSecret(web) != "" || ReleaseStorage(web) != DriverConfigMap {
		t.Errorf("record %s %s, secret %q", d, name, ReleaseSecret(web))
	}
	secs := p.Detail(web)
	props := map[string]model.DetailItem{}
	for _, it := range secs[0].Items {
		props[it.Name] = it
	}
	if props[TitleStorage].Value != "ConfigMaps" || props["Status"].Value != "Deployed" {
		t.Errorf("properties %+v", props)
	}
	last := secs[len(secs)-1]
	if h := last.Table; last.Title != TitleHistory || h == nil || len(h.Rows) != 2 || h.Rows[0][0] != "2" ||
		h.Rows[0][5] != "Revision 2" || h.Rows[1][1] != "Superseded" || h.Rows[1][5] != "Revision 1" {
		t.Errorf("history %+v", last)
	}
	if v := secs[1].Items[0]; !v.Private || !strings.Contains(v.Value, "password: hunter2") {
		t.Errorf("values %+v", v)
	}
	if withheld := model.WithholdPrivate(secs); withheld[1].Items[0].Value != model.Withheld || withheld[2].Items[0].Value != model.Withheld {
		t.Errorf("values and notes for agents %+v", withheld[1:3])
	}
	vals, err := ReleaseValues(cms[1], false)
	if err != nil || !strings.Contains(vals.YAML, "hunter2") || vals.Revision != 2 {
		t.Errorf("values from the ConfigMap: %+v %v", vals, err)
	}
	if !model.IsHelmRecord(cms[0]) || model.IsHelmRecord(secretFor(t, testRelease{ns: "apps", name: "web", rev: 1})) {
		t.Error("model.IsHelmRecord")
	}
}

// Releases of both drivers side by side: the same name in different namespaces is two releases,
// each read with its own driver; the same name in one namespace (Helm sees two releases, one per
// driver) is one object, the release deployed last, whose drawer says the other driver holds one
// too. Neither revision history leaks into the other's.
func TestDeriveMixedDrivers(t *testing.T) {
	p := NewReleaseProfile()
	objs := p.Derive([]*unstructured.Unstructured{
		secretFor(t, testRelease{ns: "apps", name: "web", rev: 1, status: common.StatusSuperseded}),
		secretFor(t, testRelease{ns: "apps", name: "web", rev: 2, status: common.StatusDeployed}),
		configMapFor(t, testRelease{ns: "other", name: "web", rev: 1, status: common.StatusDeployed}),
		// db in apps: two revisions in Secrets (the newer deployed 2h after t0), four in ConfigMaps (4h).
		secretFor(t, testRelease{ns: "apps", name: "db", rev: 1, status: common.StatusSuperseded}),
		secretFor(t, testRelease{ns: "apps", name: "db", rev: 2, status: common.StatusDeployed}),
		configMapFor(t, testRelease{ns: "apps", name: "db", rev: 3, status: common.StatusSuperseded}),
		configMapFor(t, testRelease{ns: "apps", name: "db", rev: 4, status: common.StatusFailed}),
	})
	got := byKey(objs)
	if len(objs) != 3 {
		t.Fatalf("releases %v", got)
	}
	if w := got["apps/web"]; ReleaseStorage(w) != DriverSecret || ReleaseRevision(w) != 2 || ReleaseSecret(w) != "sh.helm.release.v1.web.v2" {
		t.Errorf("apps/web %v", w.Object)
	}
	if w := got["other/web"]; ReleaseStorage(w) != DriverConfigMap || ReleaseRevision(w) != 1 {
		t.Errorf("other/web %v", w.Object)
	}
	db := got["apps/db"]
	if ReleaseStorage(db) != DriverConfigMap || ReleaseRevision(db) != 4 || ReleaseStatus(db) != "failed" ||
		!slices.Equal(ReleaseRevisions(db), []int{4, 3}) {
		t.Errorf("apps/db %v (revisions %v)", db.Object, ReleaseRevisions(db))
	}
	// The drawer's note in the owner's wording (A39): the other driver's release is the older one.
	if storage := storageOf(p, db); storage.Status != model.StatusWarning || storage.Value != "ConfigMaps; also stored in the Secrets driver (older)" {
		t.Errorf("storage %+v", storage)
	}
	if n, _ := model.NestedInt64(db, fieldHiddenRevisions); n != 2 {
		t.Errorf("hidden revisions %d", n)
	}
	if storage := storageOf(p, got["apps/web"]); storage.Status != model.StatusNone || storage.Value != "Secrets" {
		t.Errorf("a release in one driver: %+v", storage)
	}

	// The same releases in another order give the same objects (the choice does not depend on
	// the order the cache lists them in), and a tie in time goes to Helm's default, Secrets.
	again := byKey(p.Derive([]*unstructured.Unstructured{
		configMapFor(t, testRelease{ns: "apps", name: "db", rev: 4, status: common.StatusFailed}),
		secretFor(t, testRelease{ns: "apps", name: "db", rev: 2, status: common.StatusDeployed}),
	}))
	if ReleaseStorage(again["apps/db"]) != DriverConfigMap {
		t.Errorf("order changed the choice: %v", again["apps/db"].Object)
	}
	tie := byKey(p.Derive([]*unstructured.Unstructured{
		configMapFor(t, testRelease{ns: "apps", name: "db", rev: 2, status: common.StatusDeployed}),
		secretFor(t, testRelease{ns: "apps", name: "db", rev: 2, status: common.StatusDeployed}),
	}))
	if ReleaseStorage(tie["apps/db"]) != DriverSecret || model.NestedString(tie["apps/db"], fieldHidden) != "configmap" {
		t.Errorf("a tie: %v", tie["apps/db"].Object)
	}
	if storage := storageOf(p, tie["apps/db"]); storage.Status != model.StatusWarning ||
		storage.Value != "Secrets; also stored in the ConfigMaps driver (deployed at the same time; the Secrets' one is shown, Helm's default)" {
		t.Errorf("a tie's note: %+v", storage)
	}
}

// The release deployed last is shown, whichever driver holds it, and the other driver's release is
// named as the older one; the note follows when a newer release in the other driver takes the
// place (A39).
func TestDeriveBothDriversNamesTheOlder(t *testing.T) {
	p := NewReleaseProfile()
	secrets := []*unstructured.Unstructured{
		secretFor(t, testRelease{ns: "apps", name: "api", rev: 1, status: common.StatusSuperseded}),
		secretFor(t, testRelease{ns: "apps", name: "api", rev: 2, status: common.StatusSuperseded}),
		secretFor(t, testRelease{ns: "apps", name: "api", rev: 3, status: common.StatusDeployed}),
	}
	older := configMapFor(t, testRelease{ns: "apps", name: "api", rev: 1, status: common.StatusDeployed})
	api := byKey(p.Derive(append(slices.Clone(secrets), older)))["apps/api"]
	if ReleaseStorage(api) != DriverSecret || ReleaseRevision(api) != 3 {
		t.Fatalf("shown %v", api.Object)
	}
	if storage := storageOf(p, api); storage.Value != "Secrets; also stored in the ConfigMaps driver (older)" || storage.Status != model.StatusWarning {
		t.Errorf("storage %+v", storage)
	}
	// A newer release in ConfigMaps (revision 5, deployed later) takes its place: the Secrets'
	// release is the older one now.
	newer := configMapFor(t, testRelease{ns: "apps", name: "api", rev: 5, status: common.StatusDeployed})
	api = byKey(p.Derive(append(slices.Clone(secrets), older, newer)))["apps/api"]
	if ReleaseStorage(api) != DriverConfigMap || ReleaseRevision(api) != 5 {
		t.Fatalf("shown %v", api.Object)
	}
	if storage := storageOf(p, api); storage.Value != "ConfigMaps; also stored in the Secrets driver (older)" {
		t.Errorf("storage %+v", storage)
	}
}

// storageOf returns the Storage item of a release's drawer.
func storageOf(p *model.KindProfile, o *unstructured.Unstructured) model.DetailItem {
	for _, it := range p.Detail(o)[0].Items {
		if it.Name == TitleStorage {
			return it
		}
	}
	return model.DetailItem{}
}

// The memo holds ConfigMap records as it holds Secrets: decoded once per UID and resourceVersion,
// the values kept for the newest revision only, a hidden release's dropped, and records that are
// gone leaving within two generations.
func TestDeriveMemoConfigMaps(t *testing.T) {
	m := newTestMemo()
	recs := []*unstructured.Unstructured{
		configMapFor(t, testRelease{ns: "apps", name: "web", rev: 1, status: common.StatusSuperseded, values: map[string]any{"a": 1}}),
		configMapFor(t, testRelease{ns: "apps", name: "web", rev: 2, status: common.StatusDeployed, values: map[string]any{"a": 2}}),
		secretFor(t, testRelease{ns: "apps", name: "web", rev: 1, status: common.StatusDeployed, values: map[string]any{"a": 3}}),
	}
	built := heavyBuilt.Load()
	m.derive(recs)
	if n := heavyBuilt.Load() - built; n != 1 {
		t.Errorf("%d manifests parsed for one shown release", n)
	}
	v2 := types.UID("uid-cm-apps-sh.helm.release.v1.web.v2")
	first := m.cur[v2]
	if first == nil || first.heavy == nil || first.storage != DriverConfigMap {
		t.Fatalf("the newest ConfigMap revision: %+v", first)
	}
	for uid, r := range m.cur {
		if uid != v2 && r.heavy != nil {
			t.Errorf("%s keeps its values although it is not shown", uid)
		}
	}
	m.derive(recs)
	if m.cur[v2] != first {
		t.Error("an unchanged ConfigMap was decoded again")
	}
	for range 2 * memoGeneration {
		m.derive(recs[2:])
	}
	if n := m.size(); n != 1 {
		t.Errorf("the memo holds %d revisions, want 1", n)
	}
}

// Hostile release ConfigMaps never panic and still show the release from their labels.
func TestDecodeConfigMapRejectsBadInput(t *testing.T) {
	good := configMapFor(t, testRelease{ns: "apps", name: "web", rev: 1, status: common.StatusDeployed})
	for name, data := range map[string]any{
		"none":          nil,
		"not base64":    map[string]any{"release": "!!!"},
		"a Secret's":    map[string]any{"release": secretFor(t, testRelease{ns: "apps", name: "web", rev: 1}).Object["data"].(map[string]any)["release"]},
		"gzip garbage":  map[string]any{"release": "H4sIAAAA"},
		"not a mapping": "x",
	} {
		o := good.DeepCopy()
		if data == nil {
			delete(o.Object, "data")
		} else {
			o.Object["data"] = data
		}
		if _, err := DecodeRecord(o); err == nil {
			t.Errorf("%s: decoded", name)
		} else if name == "none" && !errors.Is(err, ErrNoPayload) {
			t.Errorf("%s: %v", name, err)
		}
		objs := NewReleaseProfile().Derive([]*unstructured.Unstructured{o})
		if len(objs) != 1 || model.NestedString(objs[0], "error") == "" || ReleaseStorage(objs[0]) != DriverConfigMap {
			t.Errorf("%s: %v", name, objs)
		}
	}
}

func TestParseDriver(t *testing.T) {
	for in, want := range map[string]Driver{"": DriverSecret, "secret": DriverSecret, "Secrets": DriverSecret, " configmap ": DriverConfigMap, "configmaps": DriverConfigMap} {
		if d, err := ParseDriver(in); err != nil || d != want {
			t.Errorf("%q: %s %v", in, d, err)
		}
	}
	if _, err := ParseDriver("sql"); !errors.Is(err, ErrSQLDriver) {
		t.Errorf("sql: %v", err)
	}
	if _, err := ParseDriver("memory"); err == nil {
		t.Error("memory accepted")
	}
	if DriverConfigMap.Resource() != "configmaps" || DriverSecret.Resource() != "secrets" || DriverConfigMap.Kind() != "ConfigMap" || DriverSecret.Kind() != "Secret" {
		t.Error("resources and kinds")
	}
}
