package engine

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/guided-traffic/cogpit/internal/cache"
	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// The Kyverno integration's fixtures: synthetic, shaped like Kyverno 1.17's reports on a real
// cluster (2026-10-17): one report per object named by its UID, its scope the object, results
// with properties.process, timestamps as seconds and nanos.
var (
	polrGVR  = schema.GroupVersionResource{Group: kinds.PolicyReportGroup, Version: "v1alpha2", Resource: "policyreports"}
	cpolrGVR = schema.GroupVersionResource{Group: kinds.PolicyReportGroup, Version: "v1alpha2", Resource: "clusterpolicyreports"}
)

func policyReportCRDs() []*unstructured.Unstructured {
	v := testkit.CRDVersion{Name: "v1alpha2", Served: true, Storage: true}
	return []*unstructured.Unstructured{
		testkit.CRD(kinds.PolicyReportGroup, "policyreports", "PolicyReport", true, v),
		testkit.CRD(kinds.PolicyReportGroup, "clusterpolicyreports", "ClusterPolicyReport", false, v),
	}
}

// kyvernoResult is one result of a report.
func kyvernoResult(policy, rule, result, process string) map[string]any {
	return map[string]any{
		"policy": policy, "rule": rule, "result": result, "source": "kyverno", "scored": true,
		"category": "Best Practices", "severity": "medium",
		"message":    policy + "/" + rule + ": " + result,
		"properties": map[string]any{"process": process},
		"timestamp":  map[string]any{"seconds": int64(1791720000), "nanos": int64(0)},
	}
}

// policyReport is Kyverno's report about one object (scope), named by its UID.
func policyReport(apiVersion, kind, namespace, name string, results ...map[string]any) *unstructured.Unstructured {
	uid := "uid-" + namespace + "-" + name
	rs := make([]any, len(results))
	summary := map[string]any{}
	for i, r := range results {
		rs[i] = r
		res := r["result"].(string)
		n, _ := summary[res].(int64)
		summary[res] = n + 1
	}
	reportKind, apiV := "PolicyReport", kinds.PolicyReportGroup+"/v1alpha2"
	if namespace == "" {
		reportKind = "ClusterPolicyReport"
	}
	o := testkit.Object(apiV, reportKind, namespace, uid, map[string]any{
		"scope":   map[string]any{"apiVersion": apiVersion, "kind": kind, "name": name, "namespace": namespace, "uid": uid},
		"summary": summary,
		"results": rs,
	})
	o.SetLabels(map[string]string{"app.kubernetes.io/managed-by": "kyverno"})
	return o
}

func deployment(namespace, name string) *unstructured.Unstructured {
	return testkit.Object("apps/v1", "Deployment", namespace, name, nil)
}

// kyvernoSession is a session over src with the custom kinds on, the built-in Nodes profile (it has
// a warning column) beside the engine tests' profiles.
func kyvernoSession(t *testing.T, src *testkit.MemorySource) *Session {
	t.Helper()
	nodes, _ := kinds.ByKey("nodes")
	s := newCRDSession(t, src, nodes)
	waitFor(t, "the policy report kinds", func() bool { return hasKind(s, kinds.PolicyReportsKey) && hasKind(s, kinds.ClusterPolicyReportsKey) })
	return s
}

func cellOf(snap *TableSnapshot, name, column string) model.Cell {
	ci := slices.IndexFunc(snap.Columns, func(c model.Column) bool { return c.ID == column })
	for _, r := range snap.Rows {
		if r.Ref.Name == name && ci >= 0 && ci < len(r.Cells) {
			return r.Cells[ci]
		}
	}
	return model.Cell{}
}

func hasColumn(snap *TableSnapshot, id string) bool {
	return slices.ContainsFunc(snap.Columns, func(c model.Column) bool { return c.ID == id && !c.Hidden })
}

func kyvernoStatus(s *Session) IntegrationStatus {
	for _, st := range s.IntegrationStatuses("test") {
		if st.Name == IntegrationKyverno {
			return st
		}
	}
	return IntegrationStatus{}
}

// TestKyvernoMarksViolations walks the integration through the engine: a Deployment's fail of an
// Audit policy marks its row in a warning column added after Name, a Node's fail (a cluster report)
// merges into the Nodes' own warning column, an object without a fail and another engine's report
// mark nothing, the drawer lists the violation, agents read it; a fixed object loses its mark and
// the added column goes; switched off, nothing shows and the reports are no longer held.
func TestKyvernoMarksViolations(t *testing.T) {
	src := testkit.NewMemorySource()
	for _, crd := range policyReportCRDs() {
		src.Set(crdsGVR, crd)
	}
	// Kyverno's ClusterPolicy kind served: a violation's policy links to it.
	src.Set(crdsGVR, testkit.CRD("kyverno.io", "clusterpolicies", "ClusterPolicy", false, testkit.CRDVersion{Name: "v1", Served: true, Storage: true}))
	src.Set(testkit.DeploymentsGVR, deployment("shop", "web"))
	src.Set(testkit.DeploymentsGVR, deployment("shop", "api"))
	src.Set(testkit.NodesGVR, testkit.Object("v1", "Node", "", "n1", nil))
	src.Set(polrGVR, policyReport("apps/v1", "Deployment", "shop", "web",
		kyvernoResult("require-team-label", "autogen-check-team", "fail", "background scan"),
		kyvernoResult("disallow-privileged", "autogen-no-privileged", "pass", "admission review"),
		kyvernoResult("inject-backup", "autogen-inject", "skip", "background scan")))
	other := policyReport("apps/v1", "Deployment", "shop", "api", map[string]any{"policy": "p", "result": "fail", "source": "kubewarden"})
	other.SetLabels(map[string]string{"app.kubernetes.io/managed-by": "kubewarden"})
	other.SetName("other-engine")
	src.Set(polrGVR, other)
	src.Set(cpolrGVR, policyReport("v1", "Node", "", "n1", kyvernoResult("require-node-owner", "check-owner", "fail", "background scan")))

	s := kyvernoSession(t, src)
	waitFor(t, "Kyverno found", func() bool { st := kyvernoStatus(s); return st.On && st.State == IntegrationFound })
	if st := kyvernoStatus(s); st.Where != "wgpolicyk8s.io/v1alpha2" {
		t.Errorf("status %+v", st)
	}

	openView(s, "deployments")
	snap := waitTable(t, s, "deployments", func(t *TableSnapshot) bool {
		return t.Synced && len(t.Rows) == 2 && cellOf(t, "web", "signals").Text != ""
	})
	web := cellOf(snap, "web", "signals")
	if web.Text != "Kyverno: 1 policy violation (require-team-label)" || web.Status != model.StatusWarning || web.Signals == nil || (*web.Signals)[0].Source != model.SignalKyverno || (*web.Signals)[0].Section != "require-team-label" {
		t.Errorf("web's mark %+v", web)
	}
	if c := cellOf(snap, "api", "signals"); c != (model.Cell{}) {
		t.Errorf("another engine's report marked api: %+v", c)
	}
	if got := snap.DisplayOrder(); !slices.Equal(got, []int{0, 2, 1}) {
		t.Errorf("the added Signals column shows at %v, want right after Name", got)
	}
	// The window's column commands take the added column's id.
	if p := s.WithViewColumns(testDeployments); p.ColumnIndex("signals") != 2 {
		t.Errorf("the view's columns %+v", p.Columns)
	}
	s.UpdateUI(func(u *UIState) { u.Sort = SortSpec{Column: "signals", Desc: true} })
	waitTable(t, s, "deployments", func(t *TableSnapshot) bool { return t.Sort.Column == "signals" && t.Rows[0].Ref.Name == "web" })

	// The drawer: one section per violation, under its group.
	m, err := s.Detail(model.ObjectRef{Context: "test", Kind: "deployments", Namespace: "shop", Name: "web"})
	if err != nil {
		t.Fatal(err)
	}
	sec := sectionByGroup(m.Sections, "Policy violations")
	if len(sec) != 1 || sec[0].Title != "require-team-label" {
		t.Fatalf("drawer sections %+v", m.Sections)
	}
	items := map[string]model.DetailItem{}
	for _, it := range sec[0].Items {
		items[it.Name] = it
	}
	if items["Rule"].Value != "autogen-check-team" || items["Result"].Value != "Fail" || items["Found by"].Value != "Background scan" ||
		items["Severity"].Value != "medium" || items["Category"].Value != "Best Practices" || items["Reported"].Value == "" ||
		items["Policy"].Link == nil || items["Policy"].Link.Kind != "clusterpolicies.kyverno.io" {
		t.Errorf("section items %+v", sec[0].Items)
	}
	if m, _ := s.Detail(model.ObjectRef{Context: "test", Kind: "deployments", Namespace: "shop", Name: "api"}); len(sectionByGroup(m.Sections, "Policy violations")) != 0 {
		t.Error("api has a Policy violations section")
	}

	// A cluster-scoped object's mark merges into its kind's own warning column.
	openView(s, "nodes")
	nodes := waitTable(t, s, "nodes", func(t *TableSnapshot) bool {
		return t.Synced && strings.Contains(cellOf(t, "n1", "signals").Text, "Kyverno: 1 policy violation (require-node-owner)")
	})
	if n := len(nodes.Columns); n != len(mustProfile(t, s, "nodes").Columns) {
		t.Errorf("Nodes has %d columns: a column was added beside its own warning column", n)
	}

	// Agents: every violation at once, each with its object's kind key.
	vs, err := s.PolicyViolations(context.Background(), s.Backend(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, v := range vs {
		got = append(got, v.Object.Kind+"/"+v.Object.Namespace+"/"+v.Object.Name+":"+v.Policy)
	}
	if want := []string{"nodes//n1:require-node-owner", "deployments/shop/web:require-team-label"}; !slices.Equal(got, want) {
		t.Errorf("violations %v, want %v", got, want)
	}

	// The cache keeps the report's violations only: its pass and skip are not held.
	b := s.Backend()
	h := b.Cache.Acquire(cache.Scope{GVR: polrGVR, Trim: kyvernoTrim})
	if o, ok := h.Get("shop", "uid-shop-web"); !ok || len(model.NestedSlice(o.Object, "results")) != 1 {
		t.Errorf("the trimmed report %v", o)
	}
	if o, ok := h.Get("shop", "other-engine"); !ok || model.NestedSlice(o.Object, "results") != nil {
		t.Errorf("another engine's report kept its results: %v", o)
	}
	h.Release()

	// Fixed: the mark goes, and with the last Deployment's the added column.
	src.Set(polrGVR, policyReport("apps/v1", "Deployment", "shop", "web",
		kyvernoResult("require-team-label", "autogen-check-team", "pass", "admission review")))
	openView(s, "deployments")
	waitTable(t, s, "deployments", func(t *TableSnapshot) bool { return t.Synced && len(t.Rows) == 2 && !hasColumn(t, "signals") })

	// Off: no mark, no section, the reports' scopes released.
	off := IntKyverno
	s.UpdateUI(func(u *UIState) {
		cs := u.ClusterSettings["test"]
		cs.IntegrationsOff = &off
		if u.ClusterSettings == nil {
			u.ClusterSettings = map[string]ClusterSettings{}
		}
		u.ClusterSettings["test"] = cs
	})
	openView(s, "nodes")
	waitTable(t, s, "nodes", func(t *TableSnapshot) bool {
		return t.Synced && !strings.Contains(cellOf(t, "n1", "signals").Text, "Kyverno")
	})
	if m, _ := s.Detail(model.ObjectRef{Context: "test", Kind: "nodes", Name: "n1"}); len(sectionByGroup(m.Sections, "Policy violations")) != 0 {
		t.Error("a Policy violations section while Kyverno is off")
	}
	for _, st := range b.Cache.Stats() {
		if st.Scope.Trim == kyvernoTrim && st.Handles != 0 {
			t.Errorf("%s still held while Kyverno is off", st.Scope)
		}
	}
	if _, err := s.PolicyViolations(context.Background(), b, nil); err == nil {
		t.Error("violations read while Kyverno is off")
	}
	if st := kyvernoStatus(s); st.On || st.State != IntegrationFound {
		t.Errorf("off: status %+v", st)
	}
}

func sectionByGroup(sections []model.DetailSection, group string) []model.DetailSection {
	var out []model.DetailSection
	for _, sec := range sections {
		if sec.Group == group {
			out = append(out, sec)
		}
	}
	return out
}

func mustProfile(t *testing.T, s *Session, key string) *model.KindProfile {
	t.Helper()
	p, ok := s.Backend().Profile(key)
	if !ok {
		t.Fatalf("no profile %s", key)
	}
	return p
}

// TestKyvernoMissing: a cluster that serves no policy reports says so, and nothing is read.
func TestKyvernoMissing(t *testing.T) {
	src := testkit.NewMemorySource()
	src.Set(testkit.DeploymentsGVR, deployment("shop", "web"))
	s := newCRDSession(t, src)
	b := s.Backend()
	waitFor(t, "the definitions to be listed", func() bool { set := b.customSet(); return set != nil && set.synced })
	waitFor(t, "Kyverno missing", func() bool { return kyvernoStatus(s).State == IntegrationMissing })
	if st := kyvernoStatus(s); !strings.Contains(st.Detail, "not served") {
		t.Errorf("status %+v", st)
	}
	if _, on := overlayOf[*kyvernoSource](b); on {
		t.Error("an overlay without policy reports")
	}
}

// TestViolationIndex: both report shapes (a scope; a namespace's report naming each result's
// objects, with and without a UID), the ref fallback, the per-kind counts, and a report's removal.
func TestViolationIndex(t *testing.T) {
	src := testkit.NewMemorySource()
	c := cache.New(src, cache.Options{})
	defer c.Close()
	changes := 0
	x := newViolationIndex(func() { changes++ })
	old := testkit.Object(kinds.PolicyReportGroup+"/v1alpha2", "PolicyReport", "shop", "polr-ns-shop", map[string]any{
		"results": []any{
			map[string]any{"policy": "shop/disallow-latest-tag", "rule": "tag", "result": "warn", "source": "kyverno",
				"resources": []any{
					map[string]any{"apiVersion": "v1", "kind": "Pod", "namespace": "shop", "name": "web-0", "uid": "u-web-0"},
					map[string]any{"apiVersion": "v1", "kind": "Pod", "namespace": "shop", "name": "web-1"},
				}},
			map[string]any{"policy": "verify", "rule": "sig", "result": "error", "source": "kyverno",
				"resources": []any{map[string]any{"apiVersion": "v1", "kind": "Pod", "namespace": "shop", "name": "web-0", "uid": "u-web-0"}}},
		},
	})
	src.Set(polrGVR, old)
	h := c.Acquire(cache.Scope{GVR: polrGVR})
	defer h.Release()
	ctx, cancel := context.WithTimeout(context.Background(), 5e9)
	defer cancel()
	h.WaitForSync(ctx)
	x.sync(h, kinds.PolicyReportsKey, "shop", "polr-ns-shop")

	pods, _ := kinds.ByKey("pods")
	web0 := testkit.Pod("shop", "web-0")
	web0.SetUID("u-web-0")
	web1 := testkit.Pod("shop", "web-1") // its result names no UID: found by group, Kind, namespace and name
	if vs := x.of(pods, web0); len(vs) != 1 || vs[0].Result != "warn" || vs[0].PolicyRef().Kind != "policies.kyverno.io" {
		t.Errorf("web-0: %+v (an error is not a violation)", vs)
	}
	if vs := x.of(pods, web1); len(vs) != 1 {
		t.Errorf("web-1: %+v", vs)
	}
	if !x.hasKind("", "Pod") || changes != 1 {
		t.Errorf("Pods counted: %v; changes %d", x.kinds, changes)
	}
	x.sync(h, kinds.PolicyReportsKey, "shop", "polr-ns-shop") // the same report again: no change
	if changes != 1 {
		t.Errorf("an unchanged report changed the index (%d)", changes)
	}
	src.Delete(polrGVR, "shop", "polr-ns-shop")
	waitFor(t, "the report's removal", func() bool { _, ok := h.Get("shop", "polr-ns-shop"); return !ok })
	x.sync(h, kinds.PolicyReportsKey, "shop", "polr-ns-shop")
	if x.hasKind("", "Pod") || len(x.of(pods, web0)) != 0 || len(x.byObject) != 0 || len(x.kindOf) != 0 {
		t.Errorf("after the removal: kinds %v, objects %v", x.kinds, x.byObject)
	}
}

// TestTrimPolicyReport: the trim keeps a Kyverno report's fail and warn results, drops the others,
// and every result of another engine's report.
func TestTrimPolicyReport(t *testing.T) {
	r := policyReport("apps/v1", "Deployment", "shop", "web",
		kyvernoResult("a", "r", "pass", ""), kyvernoResult("b", "r", "fail", ""), kyvernoResult("c", "r", "warn", ""),
		kyvernoResult("d", "r", "error", ""), kyvernoResult("e", "r", "skip", ""))
	trimPolicyReport(r)
	var kept []string
	for _, res := range model.NestedSlice(r.Object, "results") {
		kept = append(kept, model.NestedString(res, "policy"))
	}
	if !slices.Equal(kept, []string{"b", "c"}) || model.NestedMap(r.Object, "summary") == nil || model.NestedMap(r.Object, "scope") == nil {
		t.Errorf("kept %v of %v", kept, r.Object)
	}
	passes := policyReport("apps/v1", "Deployment", "shop", "api", kyvernoResult("a", "r", "pass", ""))
	if trimPolicyReport(passes); passes.Object["results"] != nil {
		t.Error("a report without a violation kept its results")
	}
	other := policyReport("apps/v1", "Deployment", "shop", "x", map[string]any{"policy": "p", "result": "fail", "source": "kubewarden"})
	other.SetLabels(nil)
	if trimPolicyReport(other); other.Object["results"] != nil {
		t.Error("another engine's report kept its results")
	}
}

// TestOverlayLayout: an added column shows after the column its overlay names unless the user
// placed it; a merged cell joins the reasons and keeps the worse status.
func TestOverlayLayout(t *testing.T) {
	cols := []model.Column{{ID: "name"}, {ID: "namespace"}, {ID: "age"}, {ID: "signals"}}
	l := &overlayLayout{added: map[int]string{3: "name"}}
	if got := l.order(cols, nil); !slices.Equal(got, []int{0, 3, 1, 2}) {
		t.Errorf("order %v", got)
	}
	if got := l.order(cols, []string{"age", "name"}); !slices.Equal(got, []int{2, 0, 3, 1}) {
		t.Errorf("order after the user's %v", got)
	}
	if got := l.order(cols, []string{"signals", "age"}); !slices.Equal(got, []int{3, 2, 0, 1}) {
		t.Errorf("the user's own place %v", got)
	}
	m := mergeCell(model.Cell{Text: "Not ready", Num: 1, Status: model.StatusError}, model.Cell{Text: "Kyverno: 1 policy violation (p)", Num: 1, Status: model.StatusWarning})
	if m.Text != "Not ready; Kyverno: 1 policy violation (p)" || m.Status != model.StatusError || m.Num != 1 {
		t.Errorf("merged %+v", m)
	}
	if m := mergeCell(model.Cell{}, model.Cell{Text: "x", Num: 1, Status: model.StatusWarning}); m.Text != "x" {
		t.Errorf("into an empty cell %+v", m)
	}
}

// TestMessageLines: a policy's message a sentence per line, abbreviations left alone.
func TestMessageLines(t *testing.T) {
	got := messageLines("validation error: Using a mutable image tag e.g. 'latest' is not allowed. rule x failed at path /spec/")
	if got != "validation error: Using a mutable image tag e.g. 'latest' is not allowed.\nrule x failed at path /spec/" {
		t.Errorf("%q", got)
	}
}

// TestKyvernoNamespaceLimitedUser: a user whose one RoleBinding is in the context's namespace
// team-a may list Deployments and policy reports there and nowhere else, and no cluster reports.
// The reports' read follows the refusal in all namespaces as a view's does and reads team-a alone,
// the Deployment there is marked, and the settings say which kind the user may not list.
func TestKyvernoNamespaceLimitedUser(t *testing.T) {
	mem := testkit.NewMemorySource()
	for _, crd := range policyReportCRDs() {
		mem.Set(crdsGVR, crd)
	}
	mem.Set(testkit.DeploymentsGVR, deployment("team-a", "web"))
	mem.Set(polrGVR, policyReport("apps/v1", "Deployment", "team-a", "web",
		kyvernoResult("require-team-label", "check-team", "fail", "background scan")))
	src := &testkit.NamespacedOnlySource{Inner: mem, Denied: []string{"team-b"},
		GVRs: []schema.GroupVersionResource{testkit.DeploymentsGVR, polrGVR, cpolrGVR}}
	rev := newFakeReviewer(map[string][]cluster.AccessRule{"team-a": {
		rule("get,list,watch", "apps", "deployments"), rule("get,list,watch", kinds.PolicyReportGroup, "policyreports"),
	}})
	s := New(Options{Connector: dockConnector{src: src, rev: rev, profiles: testProfiles(), namespace: "team-a", custom: true},
		SnapshotInterval: 5 * time.Millisecond})
	t.Cleanup(s.Close)
	connectTest(t, s)
	waitFor(t, "the policy report kinds", func() bool { return hasKind(s, kinds.PolicyReportsKey) })
	openView(s, "deployments")
	waitTable(t, s, "deployments", func(t *TableSnapshot) bool {
		return t.Synced && strings.HasPrefix(cellOf(t, "web", "signals").Text, "Kyverno: 1 policy violation")
	})
	waitFor(t, "the status naming the refused kind", func() bool {
		st := kyvernoStatus(s)
		return st.State == IntegrationFound && strings.Contains(st.Detail, kinds.ClusterPolicyReportsKey)
	})
	calls := callsOf(src, "policyreports")
	if calls["watch policyreports.wgpolicyk8s.io (kyverno-violations) in team-a"] == 0 {
		t.Errorf("the reports not read in team-a: %v", calls)
	}
	for _, st := range s.Backend().Cache.Stats() {
		if st.Scope.GVR == polrGVR && st.Scope.Namespace == "" && st.Handles > 0 {
			t.Errorf("a refused read in all namespaces still held: %s", st.Scope)
		}
	}
}

// Two overlays in one Signals column: the kind's own state keeps slot 0, the overlays take 1 and 2
// in their order, the column counts three, and each signal carries its slot.
func TestSignalsSlots(t *testing.T) {
	p := &model.KindProfile{Key: "pods", Kind: "Pod", Version: "v1", Columns: []model.Column{
		{ID: "name", Title: "Name", Type: model.ColumnText},
		{ID: model.SignalsColumnID, Title: model.SignalsColumnTitle, Type: model.ColumnSignals, Slots: 1},
	}}
	ky := signalOverlay{source: model.SignalKyverno, status: model.StatusWarning}
	tr := signalOverlay{source: model.SignalTrivy, grade: model.GradeD}
	l := layoutOverlays(p, []rowOverlay{ky, tr}, 1)
	if got := l.columns(p)[1].Slots; got != 3 || p.Columns[1].Slots != 1 {
		t.Fatalf("slots %d, the kind's own %d", got, p.Columns[1].Slots)
	}
	state := model.SignalsCell([]model.Signal{{Source: model.SignalState, Text: "CrashLoopBackOff", Status: model.StatusError,
		Rank: model.SignalRank(model.StatusError, model.GradeNone, 0)}})
	row, ok := l.apply(p, model.Row{Cells: []model.Cell{{Text: "web"}, state}}, nil)
	if !ok || row.Cells[1].Signals == nil {
		t.Fatalf("row %+v", row)
	}
	var slots []int
	var sources []model.SignalSource
	for _, s := range *row.Cells[1].Signals {
		slots, sources = append(slots, s.Slot), append(sources, s.Source)
	}
	if !slices.Equal(slots, []int{0, 1, 2}) || !slices.Equal(sources, []model.SignalSource{model.SignalState, model.SignalKyverno, model.SignalTrivy}) {
		t.Errorf("slots %v, sources %v", slots, sources)
	}
	if c := row.Cells[1]; c.Status != model.StatusError || c.Text != "CrashLoopBackOff; kyverno; trivy" {
		t.Errorf("merged %+v", c)
	}
}

// signalOverlay gives every object one signal of its source in the Signals column.
type signalOverlay struct {
	source model.SignalSource
	status model.Status
	grade  model.ImageGrade
}

func (o signalOverlay) columns(*model.KindProfile) []overlayColumn {
	return []overlayColumn{signalsColumn}
}
func (o signalOverlay) cell(*model.KindProfile, string, *unstructured.Unstructured) (model.Cell, bool) {
	return model.SignalsCell([]model.Signal{{Source: o.source, Text: string(o.source), Status: o.status, Grade: o.grade}}), true
}
func (o signalOverlay) sections(*model.KindProfile, *unstructured.Unstructured, model.TimeFormat) []model.DetailSection {
	return nil
}
func (o signalOverlay) waitSynced(context.Context) {}
func (o signalOverlay) stop()                      {}
