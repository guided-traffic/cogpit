package fyneui

import (
	"slices"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
	"github.com/guided-traffic/cogpit/internal/testkit"
	"github.com/guided-traffic/cogpit/internal/testkit/enginekit"
)

// The Kyverno integration's marks (D-79), over the real engine with the synthetic cluster of the
// list renders and Kyverno's reports about some of its objects: the Pods' own warning column with
// the mark merged in, the warning column added after Name to Deployments while one violates a
// policy, a node's mark (a cluster report), and a Deployment's drawer with its "Policy violations".
// PNGs: kyverno_{pods,deployments,nodes,drawer}_{dark,light}.png.

var (
	renderPolrGVR  = schema.GroupVersionResource{Group: kinds.PolicyReportGroup, Version: "v1alpha2", Resource: "policyreports"}
	renderCpolrGVR = schema.GroupVersionResource{Group: kinds.PolicyReportGroup, Version: "v1alpha2", Resource: "clusterpolicyreports"}
)

// kyvernoResultOf is one of a report's results, shaped like Kyverno 1.17's.
func kyvernoResultOf(policy, rule, result, process, severity, category, message string) any {
	return map[string]any{
		"policy": policy, "rule": rule, "result": result, "source": "kyverno", "scored": result != "warn",
		"severity": severity, "category": category, "message": message,
		"properties": map[string]any{"process": process},
		"timestamp":  map[string]any{"seconds": fixedNow.Add(-40 * time.Minute).Unix(), "nanos": int64(0)},
	}
}

// kyvernoReportOf is Kyverno's report about obj, named by its UID.
func kyvernoReportOf(obj *unstructured.Unstructured, results ...any) *unstructured.Unstructured {
	kind := "PolicyReport"
	if obj.GetNamespace() == "" {
		kind = "ClusterPolicyReport"
	}
	r := testkit.Object(kinds.PolicyReportGroup+"/v1alpha2", kind, obj.GetNamespace(), string(obj.GetUID()), map[string]any{
		"scope": map[string]any{"apiVersion": obj.GetAPIVersion(), "kind": obj.GetKind(), "namespace": obj.GetNamespace(),
			"name": obj.GetName(), "uid": string(obj.GetUID())},
		"results": results,
	})
	r.SetLabels(map[string]string{"app.kubernetes.io/managed-by": "kyverno"})
	return r
}

func kyvernoFixture(t *testing.T) *testkit.MemorySource {
	src := testkit.NewMemorySource()
	v := testkit.CRDVersion{Name: "v1alpha2", Served: true, Storage: true}
	src.Set(testkit.CRDsGVR, testkit.CRD(kinds.PolicyReportGroup, "policyreports", "PolicyReport", true, v))
	src.Set(testkit.CRDsGVR, testkit.CRD(kinds.PolicyReportGroup, "clusterpolicyreports", "ClusterPolicyReport", false, v))
	src.Set(testkit.CRDsGVR, testkit.CRD("kyverno.io", "clusterpolicies", "ClusterPolicy", false, testkit.CRDVersion{Name: "v1", Served: true, Storage: true}))
	pods, deps, nodes := fixturePodObjects(), fixtureDeploymentObjects(), fixtureNodeObjects()
	for _, o := range pods {
		src.Set(testkit.PodsGVR, o)
	}
	for _, o := range deps {
		src.Set(testkit.DeploymentsGVR, o)
	}
	for _, o := range nodes {
		src.Set(testkit.NodesGVR, o)
	}
	team := kyvernoResultOf("require-team-label", "autogen-check-team", "fail", "background scan", "medium", "Best Practices",
		"validation error: The label `team` is required. rule autogen-check-team failed at path /spec/template/metadata/labels/team/")
	latest := kyvernoResultOf("disallow-latest-tag", "autogen-validate-image-tag", "warn", "admission review", "low", "Best Practices",
		"validation error: Using a mutable image tag e.g. 'latest' is not allowed.")
	src.Set(renderPolrGVR, kyvernoReportOf(deps[0], team, latest))
	src.Set(renderPolrGVR, kyvernoReportOf(deps[1], team))
	for _, i := range []int{0, 2, 5} { // a running pod, a crash-looping one (its own warning merged), another
		src.Set(renderPolrGVR, kyvernoReportOf(pods[i], kyvernoResultOf("require-team-label", "check-team", "fail", "background scan",
			"medium", "Best Practices", "validation error: The label `team` is required.")))
	}
	src.Set(renderCpolrGVR, kyvernoReportOf(nodes[3], kyvernoResultOf("require-node-owner", "check-owner", "fail", "background scan",
		"high", "Multi-Tenancy", "validation error: Every node needs an owner label.")))
	return src
}

func renderKyverno(t *testing.T, mode ThemeMode, suffix string) {
	src := kyvernoFixture(t)
	conn := enginekit.MemoryConnector{Context: "kind-kyverno", Src: src, Profiles: kinds.Builtin(), CustomResources: true}
	s := newTestEngine(engine.Options{Connector: conn, SnapshotInterval: 5 * time.Millisecond})
	t.Cleanup(s.Close)
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	sh, w, q := newEngineShell(t, s, Options{AutoConnect: "kind-kyverno", Theme: mode})
	q.until(t, "the connection", func() bool { return sh.ui.Connection.State == engine.Connected && len(sh.kinds) > 0 })
	marked := func(view string, n int) func() bool {
		return func() bool {
			snap := sh.view.snap
			if snap == nil || snap.View != view || !snap.Synced {
				return false
			}
			ci := slices.IndexFunc(snap.Columns, func(c model.Column) bool { return c.ID == "warning" })
			got := 0
			for _, r := range snap.Rows {
				if ci >= 0 && ci < len(r.Cells) && strings.Contains(r.Cells[ci].Text, "Kyverno") {
					got++
				}
			}
			return got == n
		}
	}
	for _, sc := range []struct {
		view  string
		marks int
	}{{"pods", 3}, {"deployments", 2}, {"nodes", 1}} {
		mustDispatch(t, s, "ui.openView", map[string]any{"view": sc.view})
		q.until(t, "the "+sc.view+" view with its marks", marked(sc.view, sc.marks))
		settle(w)
		savePNG(t, w, "kyverno_"+sc.view+"_"+suffix+".png")
	}
	deps := fixtureDeploymentObjects()
	ref := model.ObjectRef{Context: "kind-kyverno", Kind: "deployments", Namespace: deps[0].GetNamespace(), Name: deps[0].GetName()}
	mustDispatch(t, s, "ui.openView", map[string]any{"view": "deployments"})
	mustDispatch(t, s, "ui.select", map[string]any{"refs": []model.ObjectRef{ref}})
	q.until(t, "the drawer's Policy violations", func() bool {
		m := sh.drawer.last
		return m != nil && m.Ref.Name == ref.Name && slices.ContainsFunc(m.Sections, func(sec model.DetailSection) bool {
			return sec.Group == "Policy violations"
		})
	})
	settle(w)
	savePNG(t, w, "kyverno_drawer_"+suffix+".png")
}

func TestRenderKyvernoDark(t *testing.T) { renderKyverno(t, ThemeDark, "dark") }
