package helm

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	chart "helm.sh/helm/v4/pkg/chart/v2"
	"helm.sh/helm/v4/pkg/release/common"
	rspb "helm.sh/helm/v4/pkg/release/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"

	"github.com/guided-traffic/cogpit/internal/model"
)

var t0 = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

// testRelease is one revision of a release, as a test writes it.
type testRelease struct {
	ns, name string
	rev      int
	status   common.Status
	values   map[string]any
	manifest string
	notes    string
}

// secretFor encodes a revision the way Helm's Secrets driver stores it: the Secret's name and
// labels, and data.release as Kubernetes base64 of Helm's own base64 of gzipped JSON.
func secretFor(t testing.TB, r testRelease) *unstructured.Unstructured {
	t.Helper()
	rls := &rspb.Release{
		Name: r.name, Namespace: r.ns, Version: r.rev, Config: r.values, Manifest: r.manifest,
		Info: &rspb.Info{
			Status: r.status, FirstDeployed: t0, LastDeployed: t0.Add(time.Duration(r.rev) * time.Hour),
			Description: fmt.Sprintf("Revision %d", r.rev), Notes: r.notes,
		},
		Chart: &chart.Chart{Metadata: &chart.Metadata{Name: "web", Version: fmt.Sprintf("1.%d.0", r.rev), AppVersion: "2.0"}},
	}
	tt, ok := t.(*testing.T)
	var payload string
	if ok {
		payload = encode(tt, rls, true)
	} else {
		b, _ := json.Marshal(rls)
		payload = base64.StdEncoding.EncodeToString(b)
	}
	name := fmt.Sprintf("sh.helm.release.v1.%s.v%d", r.name, r.rev)
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1", "kind": "Secret", "type": "helm.sh/release.v1",
		"metadata": map[string]any{
			"name": name, "namespace": r.ns, "uid": "uid-" + r.ns + "-" + name, "resourceVersion": "1",
			"creationTimestamp": t0.Add(time.Duration(r.rev) * time.Hour).Format(time.RFC3339),
			"labels":            map[string]any{"owner": "helm", "name": r.name, "version": fmt.Sprint(r.rev), "status": string(r.status)},
		},
		"data": map[string]any{"release": base64.StdEncoding.EncodeToString([]byte(payload))},
	}}
}

func newTestMemo() *memo {
	return &memo{cur: map[types.UID]*revision{}, prev: map[types.UID]*revision{}}
}

// releaseSecrets: web with three revisions in apps, web with one in other, db with one in apps.
func releaseSecrets(t *testing.T) []*unstructured.Unstructured {
	return []*unstructured.Unstructured{
		secretFor(t, testRelease{ns: "apps", name: "web", rev: 1, status: common.StatusSuperseded}),
		secretFor(t, testRelease{ns: "apps", name: "web", rev: 3, status: common.StatusDeployed,
			values: map[string]any{"password": "hunter2", "replicas": 2}, manifest: "---\n# Source: web/templates/cm.yaml\napiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: web\n"}),
		secretFor(t, testRelease{ns: "apps", name: "web", rev: 2, status: common.StatusSuperseded}),
		secretFor(t, testRelease{ns: "other", name: "web", rev: 1, status: common.StatusFailed}),
		secretFor(t, testRelease{ns: "apps", name: "db", rev: 1, status: common.StatusPendingInstall}),
	}
}

func byKey(objs []*unstructured.Unstructured) map[string]*unstructured.Unstructured {
	out := map[string]*unstructured.Unstructured{}
	for _, o := range objs {
		out[o.GetNamespace()+"/"+o.GetName()] = o
	}
	return out
}

// Derive groups the release Secrets per namespace and release name, the newest revision winning:
// its summary, its Secret's UID and resourceVersion, and the history newest first.
func TestDeriveGroupsReleasesNewestWins(t *testing.T) {
	p := NewReleaseProfile()
	secrets := releaseSecrets(t)
	objs := p.Derive(secrets)
	if len(objs) != 3 {
		t.Fatalf("%d releases, want 3", len(objs))
	}
	got := byKey(objs)
	web := got["apps/web"]
	if web == nil || got["other/web"] == nil || got["apps/db"] == nil {
		t.Fatalf("releases %v", got)
	}
	if rev, _ := model.NestedInt64(web, "revision"); rev != 3 || web.GetUID() != "uid-apps-sh.helm.release.v1.web.v3" || web.GetResourceVersion() != "1" {
		t.Errorf("web: revision %d uid %s rv %s", rev, web.GetUID(), web.GetResourceVersion())
	}
	row := model.BuildRow(p, "test", web)
	texts := make([]string, len(row.Cells))
	for i, c := range row.Cells {
		texts[i] = model.CellText(p.Columns[i], c, t0.Add(5*time.Hour))
	}
	if strings.Join(texts, "|") != "web|apps|web|3|1.3.0|2.0|Deployed|2h" || row.Status != model.StatusOK {
		t.Errorf("row %q status %q", texts, row.Status)
	}
	if model.BuildRow(p, "test", got["apps/db"]).Status != model.StatusPending ||
		model.BuildRow(p, "test", got["other/web"]).Status != model.StatusError {
		t.Error("status tokens")
	}
	history := model.NestedSlice(web, "history")
	if len(history) != 3 || model.NestedString(history[0], "status") != "deployed" || model.NestedString(history[2], "chart") != "web-1.1.0" {
		t.Errorf("history %v", history)
	}
	if b, _ := json.Marshal(objs); strings.Contains(string(b), "hunter2") || strings.Contains(string(b), "ConfigMap") {
		t.Errorf("a release object carries values or the manifest: %s", b)
	}
	if !web.GetCreationTimestamp().Time.Equal(t0) {
		t.Errorf("created %v, want the first deployment", web.GetCreationTimestamp())
	}
}

// The drawer, as Lens's (captured 2026-10-12): Properties with Chart (and its Upgrade button),
// Updated, Namespace, Version and Status as Lens writes it; the newest revision's values and the
// chart's notes (both private); the resources, one table per kind with links.
func TestReleaseDetail(t *testing.T) {
	oldClock := clock
	clock = func() time.Time { return t0.Add(3*time.Hour + 68*time.Second) }
	t.Cleanup(func() { clock = oldClock })
	p := NewReleaseProfile()
	manifest := "---\n# Source: web/templates/cm.yaml\napiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: web\n" +
		"--- {apiVersion: apps/v1, kind: Deployment, metadata: {name: web, namespace: elsewhere}}\n" +
		"---\napiVersion: rbac.authorization.k8s.io/v1\nkind: ClusterRole\nmetadata:\n  name: web-reader\n" +
		"---\napiVersion: example.com/v1\nkind: Widget\nmetadata:\n  name: w\n---\n# empty\n---\nnot: [valid\n" +
		"---\napiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: aaa\n"
	secrets := []*unstructured.Unstructured{
		secretFor(t, testRelease{ns: "apps", name: "web", rev: 2, status: common.StatusSuperseded}),
		secretFor(t, testRelease{ns: "apps", name: "web", rev: 3, status: common.StatusDeployed, notes: "Visit http://web.apps\nPassword: hunter2",
			values: map[string]any{"password": "hunter2", "replicas": 2}, manifest: manifest}),
	}
	web := byKey(p.Derive(secrets))["apps/web"]
	secs := p.Detail(web)
	titles := make([]string, len(secs))
	for i, s := range secs {
		titles[i] = s.Group + "/" + s.Title
	}
	if strings.Join(titles, ",") != "/Properties,/Values,/Notes,Resources/ClusterRole,Resources/ConfigMap,Resources/Deployment,Resources/Widget,/History" {
		t.Fatalf("sections %v", titles)
	}
	var names []string
	props := map[string]model.DetailItem{}
	for _, it := range secs[0].Items {
		names = append(names, it.Name)
		props[it.Name] = it
	}
	if strings.Join(names, ",") != "Chart,Updated,Namespace,Version,Status,Description,Storage" {
		t.Errorf("properties %v", names)
	}
	if st := props[TitleStorage]; st.Value != "Secrets" || st.Status != model.StatusNone {
		t.Errorf("storage %+v", st)
	}
	if c := props["Chart"]; c.Value != "web-1.3.0" || len(c.Actions) != 1 || c.Actions[0].Command != UpgradeTabCommand || c.Actions[0].Label != "Upgrade" ||
		string(c.Actions[0].Params) != `{"ref":{"kind":"helmreleases","namespace":"apps","name":"web"}}` {
		t.Errorf("chart %+v", c)
	}
	// Chart reads "<chart>-<version>", Namespace is plain text and Status a neutral pill (captured 2026-10-12).
	// Updated is Lens's age and time, the time a mark the engine writes in the user's zone and
	// format (D-31): "68s ago (01.10.2026 17:00:00)" in Germany.
	de := model.TimeLayout{Date: "02.01.2006", Time: "15:04:05"}.In(time.FixedZone("CEST", 2*60*60))
	if got := de.WriteMarks(props["Updated"].Value); got != "68s ago (01.10.2026 17:00:00)" {
		t.Errorf("updated %q", got)
	}
	if props["Status"].Value != "Deployed" || !props["Status"].Pill || props["Status"].Status != model.StatusNone ||
		props["Version"].Value != "1.3.0" || props["Namespace"].Value != "apps" || props["Namespace"].Link != nil {
		t.Errorf("properties %+v", props)
	}
	if d := props["Description"]; d.Value != "Revision 3" || d.Status != model.StatusNone || !d.Wrap {
		t.Errorf("description %+v", d)
	}
	// History: every revision newest first, as helm history lists them, the description under its
	// row, the time a mark.
	h := secs[7].Table
	if h == nil || !h.LastUnder || strings.Join(h.Columns, ",") != "Revision,Status,Chart,App Version,Updated,Description" || len(h.Rows) != 2 {
		t.Fatalf("history %+v", h)
	}
	if got := strings.Join(h.Rows[0][:4], ",") + "," + de.WriteMarks(h.Rows[0][4]) + "," + h.Rows[0][5]; got != "3,Deployed,web-1.3.0,2.0,01.10.2026 17:00:00,Revision 3" {
		t.Errorf("history's newest row %q", got)
	}
	if h.Rows[1][0] != "2" || h.Rows[1][1] != "Superseded" || h.Cells[1][1].Status != model.StatusNone || h.Cells[0][1].Status != model.StatusOK {
		t.Errorf("history's rows %v", h.Rows)
	}
	values := secs[1].Items[0]
	if !values.Private || !strings.Contains(values.Value, "password: hunter2") || !strings.Contains(values.Value, "replicas: 2") {
		t.Errorf("values %+v", values)
	}
	if notes := secs[2].Items[0]; !notes.Private || notes.Value != "Visit http://web.apps\nPassword: hunter2" {
		t.Errorf("notes %+v", notes)
	}
	cm := secs[4].Table
	if cm == nil || len(cm.Rows) != 2 || cm.Rows[0][0] != "aaa" || cm.Rows[1][1] != "apps" ||
		cm.Link(1, 0) == nil || cm.Link(1, 0).Kind != "configmaps" || cm.Link(1, 1) == nil || cm.Link(1, 1).Kind != "namespaces" {
		t.Errorf("config maps %+v", cm)
	}
	if cr := secs[3].Table; cr.Rows[0][1] != "" || cr.Link(0, 0) == nil || cr.Link(0, 0).Namespace != "" {
		t.Errorf("a cluster role has a namespace: %+v", cr)
	}
	if d := secs[5].Table; d.Rows[0][1] != "elsewhere" || d.Link(0, 0).Kind != "deployments" {
		t.Errorf("deployment %+v", d)
	}
	if w := secs[6].Table; w.Rows[0][1] != "apps" || w.Link(0, 0) != nil {
		t.Errorf("a custom kind %+v", w)
	}
	withheld := model.WithholdPrivate(secs)
	if withheld[1].Items[0].Value != model.Withheld || withheld[2].Items[0].Value != model.Withheld {
		t.Errorf("values and notes for agents %+v", withheld[1:3])
	}
}

// A failed revision a rollback covered stays in the History, its status and Helm's error in the
// error colour, as k9s's history view shows it (the owner's colleague, 2026-10-25); a failed newest
// revision's description is in Properties too, plain as the other values.
func TestReleaseHistoryShowsAFailedUpgrade(t *testing.T) {
	failed := secretFor(t, testRelease{ns: "apps", name: "web", rev: 2, status: common.StatusFailed})
	rls, err := DecodeSecret(failed)
	if err != nil {
		t.Fatal(err)
	}
	msg := `Upgrade "web" failed: client rate limiter Wait returned an error: context deadline exceeded`
	rls.Info.Description = msg
	failed.Object["data"] = map[string]any{"release": base64.StdEncoding.EncodeToString([]byte(encode(t, rls, true)))}
	p := NewReleaseProfile()
	recs := []*unstructured.Unstructured{
		secretFor(t, testRelease{ns: "apps", name: "web", rev: 1, status: common.StatusSuperseded}),
		failed,
		secretFor(t, testRelease{ns: "apps", name: "web", rev: 3, status: common.StatusDeployed}),
	}
	secs := p.Detail(byKey(p.Derive(recs))["apps/web"])
	h := secs[len(secs)-1].Table
	if h == nil || len(h.Rows) != 3 || h.Rows[1][0] != "2" || h.Rows[1][1] != "Failed" || h.Rows[1][5] != msg ||
		h.Cells[1][1].Status != model.StatusError || h.Cells[1][5].Status != model.StatusError || h.Cells[0][5].Status != model.StatusNone {
		t.Fatalf("history %+v", h)
	}
	var desc model.DetailItem
	for _, it := range p.Detail(byKey(p.Derive(recs[:2]))["apps/web"])[0].Items {
		if it.Name == "Description" {
			desc = it
		}
	}
	if desc.Value != msg || desc.Status != model.StatusNone {
		t.Errorf("a failed newest revision's description %+v", desc)
	}
}

// A failed revision's description, which Helm writes from the error and the API server's error
// can quote a value in, reaches the release object scrubbed of the revision's values.
func TestReleaseDescriptionScrubbed(t *testing.T) {
	s := secretFor(t, testRelease{ns: "apps", name: "web", rev: 2, status: common.StatusFailed, values: map[string]any{"host": "hunter2.example"}})
	rls, err := DecodeSecret(s)
	if err != nil {
		t.Fatal(err)
	}
	rls.Info.Description = `Upgrade "web" failed: Ingress.networking.k8s.io "web" is invalid: spec.rules[0].host: Invalid value: "hunter2.example"`
	s.Object["data"] = map[string]any{"release": base64.StdEncoding.EncodeToString([]byte(encode(t, rls, true)))}
	web := NewReleaseProfile().Derive([]*unstructured.Unstructured{s})[0]
	if d := model.NestedString(web, "description"); strings.Contains(d, "hunter2") || !strings.Contains(d, "Invalid value") {
		t.Errorf("description %q", d)
	}
	if h := ReleaseHistory(web); strings.Contains(h[0].Description, "hunter2") {
		t.Errorf("history %q", h[0].Description)
	}
}

// Statuses as Lens writes them: capitalized.
func TestStatusText(t *testing.T) {
	for in, want := range map[string]string{"deployed": "Deployed", "pending-upgrade": "Pending-upgrade", "": "", "ünicode": "Ünicode"} {
		if got := StatusText(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}

// The history of a release object, newest first, with each revision's chart, and the Rollback
// dialog's text for one revision, as Lens's (captured 2026-10-12).
func TestReleaseHistoryAndRevisionText(t *testing.T) {
	web := byKey(NewReleaseProfile().Derive(releaseSecrets(t)))["apps/web"]
	h := ReleaseHistory(web)
	if len(h) != 3 || h[0].Revision != 3 || h[0].Chart != "web" || h[0].ChartVersion != "1.3.0" || h[0].AppVersion != "2.0" ||
		h[0].Status != "deployed" || !h[0].Updated.Equal(t0.Add(3*time.Hour)) || h[2].Revision != 1 {
		t.Fatalf("history %+v", h)
	}
	// Lens's text, its time in the user's zone and format (D-31).
	en := model.TimeLayout{Date: "1/2/2006", Time: "3:04:05 PM"}.In(time.FixedZone("CEST", 2*60*60))
	if got := RevisionText(h[0], en); got != "3 - web-1.3.0 - 2.0, updated: 10/1/2026 5:00:00 PM" {
		t.Errorf("revision text %q", got)
	}
	if name, version := ReleaseChart(web); name != "web" || version != "1.3.0" || ReleaseSecret(web) != "sh.helm.release.v1.web.v3" {
		t.Errorf("chart %s %s secret %s", name, version, ReleaseSecret(web))
	}
}

// The manifest's documents: separators with content after them, comments, and nothing copied line
// by line.
func TestSplitDocuments(t *testing.T) {
	docs := splitDocuments("a: 1\n---\nb: 2\n--- {c: 3}\n---  # comment\nd: 4\n---")
	want := []string{"a: 1\n", "b: 2\n", "{c: 3}\n", "d: 4\n", ""}
	if strings.Join(docs, "|") != strings.Join(want, "|") {
		t.Errorf("documents %q, want %q", docs, want)
	}
	if n := len(manifestResources(strings.Repeat("---\napiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: x\n", maxResources+5), "apps")); n != 1 {
		t.Errorf("%d resources of one repeated object", n)
	}
}

// The memo decodes a Secret once per resourceVersion, keeps values and manifests for the newest
// revision of each release only, decodes an older revision again when it becomes the newest, and
// forgets Secrets that are gone after two generations.
func TestDeriveMemo(t *testing.T) {
	m := newTestMemo()
	secrets := releaseSecrets(t)
	built := heavyBuilt.Load()
	releases := m.derive(secrets)
	// The manifest, notes and values are read for the newest revision of each release only, not for
	// every revision (review of the fourth run, R-13).
	if n := heavyBuilt.Load() - built; n != int64(len(releases)) || len(releases) >= len(secrets) {
		t.Errorf("%d manifests parsed for %d releases of %d revisions", n, len(releases), len(secrets))
	}
	v3 := types.UID("uid-apps-sh.helm.release.v1.web.v3")
	first := m.cur[v3]
	if first == nil || first.heavy == nil {
		t.Fatal("the newest revision is not kept with its values")
	}
	for uid, r := range m.cur {
		if uid != v3 && r.name == "web" && r.namespace == "apps" && r.heavy != nil {
			t.Errorf("%s keeps its values although it is not the newest", uid)
		}
	}
	m.derive(secrets)
	if m.cur[v3] != first {
		t.Error("an unchanged Secret was decoded again")
	}

	// A new resourceVersion is decoded again.
	changed := secrets[1].DeepCopy()
	changed.SetResourceVersion("2")
	m.derive(append([]*unstructured.Unstructured{changed}, secrets[0], secrets[2]))
	if m.cur[v3] == first || m.cur[v3].rv != "2" {
		t.Error("a changed Secret was not decoded again")
	}

	// The newest revision is deleted: revision 2 becomes the newest and gets its values back.
	objs := m.derive([]*unstructured.Unstructured{secrets[0], secrets[2]})
	if len(objs) != 1 {
		t.Fatalf("%d releases", len(objs))
	}
	if rev, _ := model.NestedInt64(objs[0], "revision"); rev != 2 {
		t.Errorf("revision %d after the newest was deleted", rev)
	}
	if r := m.cur["uid-apps-sh.helm.release.v1.web.v2"]; r == nil || r.heavy == nil {
		t.Error("the new newest revision has no values")
	}

	// Secrets that are gone leave the memo within two generations.
	for range 2 * memoGeneration {
		m.derive(secrets[:1])
	}
	if n := m.size(); n != 1 {
		t.Errorf("the memo holds %d revisions, want 1", n)
	}
}

// The memo forgets the decoded values and manifests once nobody has asked for the releases for
// memoIdle: Derive runs on every snapshot and lookup while a view, drawer or agent reads them, and
// stops when the release Secrets are no longer watched. Without the purge, the values of every
// release read once stayed in memory for as long as the cluster was connected (H-8 says: as long
// as they are watched). Asked again, it decodes them again.
func TestDeriveMemoForgetsWhenIdle(t *testing.T) {
	old := memoIdle
	memoIdle = 20 * time.Millisecond
	t.Cleanup(func() { memoIdle = old })
	m := newTestMemo()
	secrets := releaseSecrets(t)
	m.derive(secrets)
	if m.size() == 0 {
		t.Fatal("nothing memoized")
	}
	deadline := time.Now().Add(5 * time.Second)
	for m.size() != 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if n := m.size(); n != 0 {
		t.Fatalf("the memo holds %d revisions after it was idle", n)
	}
	objs := m.derive(secrets)
	if len(objs) == 0 || m.heavyOf(objs[0]) == nil {
		t.Error("the releases are not decoded again after the purge")
	}
}

// Hostile release Secrets never panic and never hide a release: one that does not decode shows
// with what its labels and name say, and the error.
func TestDeriveToleratesHostilePayloads(t *testing.T) {
	good := secretFor(t, testRelease{ns: "apps", name: "web", rev: 1, status: common.StatusDeployed})
	mk := func(name string, labels map[string]any, data any) *unstructured.Unstructured {
		o := good.DeepCopy()
		o.SetName(name)
		o.SetUID(types.UID("uid-" + name))
		o.Object["metadata"].(map[string]any)["labels"] = labels
		if data == nil {
			delete(o.Object, "data")
		} else {
			o.Object["data"] = data
		}
		return o
	}
	hostile := []*unstructured.Unstructured{
		mk("sh.helm.release.v1.api.v4", map[string]any{"owner": "helm", "name": "api", "version": "4", "status": "deployed"},
			map[string]any{"release": "!!!not base64"}),
		mk("sh.helm.release.v1.gz.v2", nil, map[string]any{"release": base64.StdEncoding.EncodeToString(
			[]byte(base64.StdEncoding.EncodeToString([]byte{0x1f, 0x8b, 0x08, 0, 1, 2, 3})))}),
		mk("sh.helm.release.v1.json.v1", map[string]any{"owner": "helm"}, map[string]any{"release": base64.StdEncoding.EncodeToString(
			[]byte(base64.StdEncoding.EncodeToString([]byte(`{"name": 7, "info": "x", "chart": [1]}`))))}),
		mk("weird", map[string]any{"owner": "helm", "version": "x"}, nil),
		mk("sh.helm.release.v1.data-not-map.v1", nil, "a string"),
		{Object: map[string]any{"metadata": "not a map"}},
		nil,
	}
	p := NewReleaseProfile()
	objs := p.Derive(hostile)
	got := byKey(objs)
	api := got["apps/api"]
	if api == nil || model.NestedString(api, "status") != "deployed" || !strings.Contains(model.NestedString(api, "error"), "base64") {
		t.Errorf("api from its labels: %v", api)
	}
	if got["apps/gz"] == nil || got["apps/weird"] == nil {
		t.Errorf("releases %v", got)
	}
	for _, o := range objs {
		model.BuildRow(p, "test", o)
		for _, s := range p.Detail(o) {
			_ = s
		}
	}
}

func FuzzDerive(f *testing.F) {
	f.Add([]byte(`{"name":"web","version":1,"info":{"status":"deployed"}}`), "web", "1", "deployed")
	f.Add([]byte("H4sI"), "", "x", "")
	f.Fuzz(func(_ *testing.T, payload []byte, name, version, status string) {
		o := &unstructured.Unstructured{Object: map[string]any{
			"metadata": map[string]any{"name": "sh.helm.release.v1." + name + ".v" + version, "namespace": "apps", "uid": "u",
				"labels": map[string]any{"owner": "helm", "name": name, "version": version, "status": status}},
			"data": map[string]any{"release": base64.StdEncoding.EncodeToString([]byte(base64.StdEncoding.EncodeToString(payload)))},
		}}
		p := NewReleaseProfile()
		for _, r := range p.Derive([]*unstructured.Unstructured{o, o}) {
			model.BuildRow(p, "fuzz", r)
			p.Detail(r)
		}
	})
}

func TestCutKeepsLinesAndSaysSo(t *testing.T) {
	text := strings.Repeat("line äöü\n", 1000)
	got := cut(text, 2000, 50)
	if strings.Count(got, "\n") > 51 || !strings.Contains(got, "of 1001 lines") {
		t.Errorf("cut: %d lines, ends %q", strings.Count(got, "\n"), got[len(got)-60:])
	}
	if cut("short", 100, 10) != "short" {
		t.Error("short text changed")
	}
}

func TestParseSecretName(t *testing.T) {
	for in, want := range map[string]string{
		"sh.helm.release.v1.web.v3":        "web 3",
		"sh.helm.release.v1.my.app.v12":    "my.app 12",
		"sh.helm.release.v1.web":           "web 0",
		"configmap":                        " 0",
		"sh.helm.release.v1.web.vbeta":     "web.vbeta 0",
		"sh.helm.release.v1.web-v2.v7":     "web-v2 7",
		"sh.helm.release.v1.with.v.dot.v2": "with.v.dot 2",
	} {
		n, r := parseSecretName(in)
		if got := fmt.Sprintf("%s %d", n, r); got != want {
			t.Errorf("%s: %q, want %q", in, got, want)
		}
	}
}

func TestStatusTokensAndPending(t *testing.T) {
	for status, want := range map[string]model.Status{
		"deployed": model.StatusOK, "failed": model.StatusError, "pending-install": model.StatusPending,
		"pending-upgrade": model.StatusPending, "pending-rollback": model.StatusPending, "uninstalling": model.StatusTerminating,
		"superseded": model.StatusNone, "uninstalled": model.StatusNone, "": model.StatusUnknown, "bogus": model.StatusUnknown,
	} {
		if got := StatusToken(status); got != want {
			t.Errorf("%q: %q, want %q", status, got, want)
		}
	}
	if !Pending("pending-rollback") || !Pending("uninstalling") || Pending("deployed") || Pending("failed") {
		t.Error("Pending")
	}
}
