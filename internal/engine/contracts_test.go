package engine

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/guided-traffic/cogpit/internal/cache"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// The contracts of the third implementation run's Phase 0 (architecture §4.2, §4.4, §5): cache
// scopes from the profile, virtual and local kinds, the related-objects index, access reviews in
// Enabled.

var claimsGVR = schema.GroupVersionResource{Version: "v1", Resource: "persistentvolumeclaims"}

// podsByClaim is a test index: pods by the claims they mount.
var podsByClaim = model.RelatedIndex{Kind: "pods", Name: "claim", Keys: func(o *unstructured.Unstructured) []string {
	var keys []string
	for _, v := range model.NestedSlice(o, "spec", "volumes") {
		if c := model.NestedString(v, "persistentVolumeClaim", "claimName"); c != "" {
			keys = append(keys, o.GetNamespace()+"/"+c)
		}
	}
	return keys
}}

var testClaims = &model.KindProfile{
	Key: "persistentvolumeclaims", Version: "v1", Resource: "persistentvolumeclaims", Kind: "PersistentVolumeClaim",
	Title: "Persistent Volume Claims", Namespaced: true,
	Columns: []model.Column{{ID: "name", Title: "Name"}, {ID: "pods", Title: "Pods"}},
	Needs:   []model.RelatedIndex{podsByClaim},
	CellsWith: func(o *unstructured.Unstructured, rel model.Related) ([]model.Cell, model.Status) {
		pods := rel.Objects(podsByClaim, o.GetNamespace()+"/"+o.GetName())
		names := make([]string, 0, len(pods))
		for _, p := range pods {
			names = append(names, p.GetName())
		}
		return []model.Cell{{Text: o.GetName()}, {Text: strings.Join(sortedStrings(names), ",")}}, model.StatusOK
	},
	DetailWith: func(o *unstructured.Unstructured, rel model.Related) []model.DetailSection {
		return []model.DetailSection{{Title: "Pods", Items: []model.DetailItem{
			{Value: strconv.Itoa(len(rel.Objects(podsByClaim, o.GetNamespace()+"/"+o.GetName())))},
		}}}
	},
}

func sortedStrings(in []string) []string {
	out := append([]string(nil), in...)
	for i := range out {
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

func podWithClaim(ns, name, claim string) *unstructured.Unstructured {
	p := testkit.Pod(ns, name)
	_ = unstructured.SetNestedSlice(p.Object, []any{map[string]any{
		"name": "data", "persistentVolumeClaim": map[string]any{"claimName": claim},
	}}, "spec", "volumes")
	return p
}

// A cell from another kind's objects follows those objects: the claim's row is rebuilt when a pod
// starts mounting it, although the claim itself did not change.
func TestRelatedIndexFillsCellsAndFollowsChanges(t *testing.T) {
	src := testkit.NewMemorySource()
	src.Set(claimsGVR, testkit.Object("v1", "PersistentVolumeClaim", "apps", "data", nil))
	src.Set(testkit.PodsGVR, podWithClaim("apps", "db-0", "data"))
	src.Set(testkit.PodsGVR, testkit.Pod("apps", "web"))
	s, fc := newTestSession(t, src, 5*time.Millisecond)
	fc.profiles = append(testProfiles(), testClaims)
	connectTest(t, s)
	s.UpdateUI(func(u *UIState) { u.View = testClaims.Key })

	cell := func(ts *TableSnapshot) string {
		if len(ts.Rows) != 1 {
			return "?"
		}
		return ts.Rows[0].Cells[1].Text
	}
	waitTable(t, s, testClaims.Key, func(ts *TableSnapshot) bool { return ts.Synced && cell(ts) == "db-0" })
	src.Set(testkit.PodsGVR, podWithClaim("apps", "db-1", "data"))
	waitTable(t, s, testClaims.Key, func(ts *TableSnapshot) bool { return cell(ts) == "db-0,db-1" })
	src.Delete(testkit.PodsGVR, "apps", "db-0")
	waitTable(t, s, testClaims.Key, func(ts *TableSnapshot) bool { return cell(ts) == "db-1" })

	m, err := s.DetailWait(context.Background(), model.ObjectRef{Kind: testClaims.Key, Namespace: "apps", Name: "data"})
	if err != nil {
		t.Fatal(err)
	}
	if got := sectionValue(m, "Pods"); got != "1" {
		t.Errorf("drawer's Pods section: %q, want 1", got)
	}

	// Outside a view (resources.list), rows carry the related objects too.
	objs, err := s.Objects(context.Background(), testClaims.Key, nil)
	if err != nil {
		t.Fatal(err)
	}
	rows := s.BuildRows(context.Background(), s.Backend(), testClaims, objs)
	if len(rows) != 1 || rows[0].Cells[1].Text != "db-1" {
		t.Errorf("BuildRows: %+v", rows)
	}
}

func sectionValue(m *DetailModel, title string) string {
	for _, sec := range m.Sections {
		if sec.Title == title && len(sec.Items) > 0 {
			return sec.Items[0].Value
		}
	}
	return ""
}

var testForwards = &model.KindProfile{
	Key: "portforwards", Group: model.LocalGroup, Version: "v1", Resource: "portforwards", Kind: "PortForward",
	Title: "Port Forwarding", Namespaced: true, Local: true,
	Columns: []model.Column{{ID: "name", Title: "Name"}, {ID: "port", Title: "Port"}},
	Cells: func(o *unstructured.Unstructured) ([]model.Cell, model.Status) {
		return []model.Cell{{Text: o.GetName()}, {Text: model.NestedString(o, "spec", "port")}}, model.StatusOK
	},
}

// A local kind lives in the backend's store: rows and reads come from it, and the cluster's source
// is never asked for it.
func TestLocalKindIsServedFromTheLocalStore(t *testing.T) {
	src := testkit.NewMemorySource()
	s, fc := newTestSession(t, src, 5*time.Millisecond)
	fc.profiles = append(testProfiles(), testForwards)
	connectTest(t, s)
	waitTable(t, s, "pods", func(ts *TableSnapshot) bool { return ts.Synced }) // the default view's list
	b := s.Backend()
	listsBefore := src.Lists()

	fw := &unstructured.Unstructured{Object: map[string]any{"spec": map[string]any{"port": "8080"}}}
	fw.SetNamespace("apps")
	fw.SetName("web-8080")
	fw.SetUID("fw-1")
	if err := b.PutLocal(testForwards, fw); err != nil {
		t.Fatal(err)
	}
	s.UpdateUI(func(u *UIState) { u.View = testForwards.Key })
	waitTable(t, s, testForwards.Key, func(ts *TableSnapshot) bool {
		return ts.Synced && len(ts.Rows) == 1 && ts.Rows[0].Cells[1].Text == "8080"
	})
	obj, err := s.Object(context.Background(), model.ObjectRef{Kind: testForwards.Key, Namespace: "apps", Name: "web-8080"})
	if err != nil {
		t.Fatal(err)
	}
	if obj.GetAPIVersion() != "cogpit.io/v1" || obj.GetKind() != "PortForward" {
		t.Errorf("apiVersion %q kind %q", obj.GetAPIVersion(), obj.GetKind())
	}
	if err := b.DeleteLocal(testForwards, "apps", "web-8080"); err != nil {
		t.Fatal(err)
	}
	waitTable(t, s, testForwards.Key, func(ts *TableSnapshot) bool { return len(ts.Rows) == 0 })
	if src.Lists() != listsBefore {
		t.Errorf("the cluster's source was listed %d times for a local kind", src.Lists()-listsBefore)
	}
	if err := b.PutLocal(testPods, fw); err == nil {
		t.Error("PutLocal accepted a cluster kind")
	}
	if testForwards.APIObject() || !testPods.APIObject() {
		t.Error("APIObject")
	}
}

// releasesKind derives one object per release from labelled Secrets, the newest revision winning.
var releasesKind = &model.KindProfile{
	Key: "releases", Version: "v1", Resource: "secrets", Kind: "Release", Title: "Releases", Namespaced: true,
	LabelSelector: "owner=helm",
	Columns:       []model.Column{{ID: "name", Title: "Name"}, {ID: "revision", Title: "Revision"}},
	Cells: func(o *unstructured.Unstructured) ([]model.Cell, model.Status) {
		return []model.Cell{{Text: o.GetName()}, {Text: model.NestedString(o, "revision")}}, model.StatusOK
	},
	Derive: func(objs []*unstructured.Unstructured) []*unstructured.Unstructured {
		latest := map[string]*unstructured.Unstructured{}
		rev := map[string]int{}
		for _, o := range objs {
			name := o.GetLabels()["name"]
			v, _ := strconv.Atoi(o.GetLabels()["version"])
			key := o.GetNamespace() + "/" + name
			if v > rev[key] {
				rev[key] = v
				r := &unstructured.Unstructured{Object: map[string]any{"revision": strconv.Itoa(v)}}
				r.SetNamespace(o.GetNamespace())
				r.SetName(name)
				r.SetUID(o.GetUID())
				r.SetResourceVersion(o.GetResourceVersion())
				latest[key] = r
			}
		}
		out := make([]*unstructured.Unstructured, 0, len(latest))
		for _, r := range latest {
			out = append(out, r)
		}
		return out
	},
}

func releaseSecret(ns, name string, rev int) *unstructured.Unstructured {
	return testkit.Secret(ns, "sh.helm.release.v1."+name+".v"+strconv.Itoa(rev),
		map[string]string{"owner": "helm", "name": name, "version": strconv.Itoa(rev)}, map[string]string{"release": "x"})
}

// A virtual kind's objects are what Derive makes of its label-selected scope; reads by name find
// the derived objects, and another scope of the same resource is another informer.
func TestVirtualKindDerivesFromItsScope(t *testing.T) {
	src := testkit.NewMemorySource()
	src.Set(testkit.SecretsGVR, releaseSecret("apps", "web", 1))
	src.Set(testkit.SecretsGVR, releaseSecret("apps", "web", 2))
	src.Set(testkit.SecretsGVR, testkit.Secret("apps", "db", nil, map[string]string{"password": "x"}))
	metaSecrets := *testSecrets
	metaSecrets.MetadataOnly = true
	s, fc := newTestSession(t, src, 5*time.Millisecond)
	fc.profiles = []*model.KindProfile{testPods, &metaSecrets, releasesKind}
	connectTest(t, s)

	s.UpdateUI(func(u *UIState) { u.View = releasesKind.Key })
	waitTable(t, s, releasesKind.Key, func(ts *TableSnapshot) bool {
		return ts.Synced && len(ts.Rows) == 1 && ts.Rows[0].Ref.Name == "web" && ts.Rows[0].Cells[1].Text == "2"
	})
	ref := model.ObjectRef{Kind: releasesKind.Key, Namespace: "apps", Name: "web"}
	if _, err := s.Object(context.Background(), ref); err != nil {
		t.Fatalf("Object of a derived kind: %v", err)
	}
	src.Set(testkit.SecretsGVR, releaseSecret("apps", "web", 3))
	waitTable(t, s, releasesKind.Key, func(ts *TableSnapshot) bool { return len(ts.Rows) == 1 && ts.Rows[0].Cells[1].Text == "3" })

	s.UpdateUI(func(u *UIState) { u.View = metaSecrets.Key })
	waitTable(t, s, metaSecrets.Key, func(ts *TableSnapshot) bool { return ts.Synced && len(ts.Rows) == 4 })
	objs, err := s.Objects(context.Background(), metaSecrets.Key, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range objs {
		if _, has := o.Object["data"]; has || !cache.IsMetadataObject(o) {
			t.Errorf("%s: the metadata-only kind holds %v", o.GetName(), o.Object)
		}
	}
	scopes := map[string]bool{}
	for _, st := range s.Backend().Cache.Stats() {
		scopes[st.Scope.String()] = true
	}
	if !scopes["secrets with owner=helm"] || !scopes["secrets (metadata)"] {
		t.Errorf("scopes: %v", scopes)
	}
	if releasesKind.APIObject() {
		t.Error("a virtual kind reports itself as an API object")
	}
}

// fakeAccess denies the verbs it lists and knows nothing else.
type fakeAccess struct{ deny map[string]bool }

func (f fakeAccess) Allowed(namespace string, v Verb) (bool, bool) {
	key := v.Verb + " " + v.Resource + "/" + v.Subresource + " " + namespace
	if f.deny[key] {
		return false, true
	}
	if v.Verb == "get" {
		return true, true
	}
	return false, false // unknown: the server decides
}

type accessParams struct {
	Ref model.ObjectRef `json:"ref"`
}

// A command whose declared verbs the access reviews deny is disabled with ErrNotPermitted and the
// reason, for every origin; unknown answers leave it enabled; commands without a ref are not
// asked about.
func TestAccessReviewsDisableCommands(t *testing.T) {
	src := testkit.NewMemorySource()
	src.Set(testkit.PodsGVR, testkit.Pod("apps", "web"))
	s, fc := newTestSession(t, src, 5*time.Millisecond)
	for _, spec := range []Spec[accessParams]{
		{ID: "test.evict", Danger: DangerRead, Verbs: []Verb{{Verb: "create", Resource: "pods", Subresource: "eviction"}}},
		{ID: "test.del", Danger: DangerRead, Verbs: []Verb{{Verb: "delete", Resource: "*"}}},
		{ID: "test.logs", Danger: DangerRead, Verbs: []Verb{{Verb: "get", Resource: "pods", Subresource: "log"}}},
	} {
		spec.Run = func(context.Context, *Session, accessParams) (any, error) { return "ran", nil }
		if err := Register(s.Registry(), spec); err != nil {
			t.Fatal(err)
		}
	}
	if err := Register(s.Registry(), Spec[struct{}]{ID: "test.noref", Danger: DangerRead, Verbs: []Verb{{Verb: "delete", Resource: "pods"}},
		Run: func(context.Context, *Session, struct{}) (any, error) { return "ran", nil }}); err != nil {
		t.Fatal(err)
	}
	fc.access = fakeAccess{deny: map[string]bool{
		"create pods/eviction apps": true,
		"delete pods/ apps":         true,
		"delete pods/ ":             true,
	}}
	connectTest(t, s)
	params, _ := json.Marshal(accessParams{Ref: model.ObjectRef{Kind: "pods", Namespace: "apps", Name: "web"}})
	for id, wantDenied := range map[string]bool{"test.evict": true, "test.del": true, "test.logs": false, "test.noref": false} {
		cmd, _ := s.Registry().Get(id)
		p := params
		if id == "test.noref" {
			p = nil
		}
		err := cmd.Enabled(s, p)
		if denied := errors.Is(err, ErrNotPermitted); denied != wantDenied {
			t.Errorf("%s: Enabled = %v, want denied %v", id, err, wantDenied)
		}
		_, derr := s.Dispatch(context.Background(), Invocation{ID: id, Params: p, Origin: OriginExternalAgent})
		var dis *DisabledError
		if wantDenied != errors.As(derr, &dis) {
			t.Errorf("%s: Dispatch = %v", id, derr)
		}
	}
	cmd, _ := s.Registry().Get("test.del")
	if err := cmd.Enabled(s, params); err == nil || !strings.Contains(err.Error(), "you may not delete pods in apps") {
		t.Errorf("reason: %v", err)
	}
}
