package fyneui

import (
	"slices"
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

// The Pods list's Image Rating (D-78's second phase, D-new's grades), over the real engine with the
// synthetic pods of the list renders and Trivy Operator's reports of most of their ReplicaSets: each
// pod's worst image grade as a shield in the untitled column after Containers, S to E; the pods of a
// Job, a DaemonSet and a node have no report and are N/A. PNGs: trivy_pods_{dark,light}.png.

var renderVulnGVR = schema.GroupVersionResource{Group: "aquasecurity.github.io", Version: "v1alpha1", Resource: "vulnerabilityreports"}

// trivyPodsFixture is the synthetic pods, the reports' definition, and a report per container of
// every ReplicaSet's pods, graded in turn E, D, C, B, A, S, and C for an OS at its end of life.
func trivyPodsFixture() *testkit.MemorySource {
	src := testkit.NewMemorySource()
	src.Set(testkit.CRDsGVR, testkit.CRD("aquasecurity.github.io", "vulnerabilityreports", "VulnerabilityReport", true,
		testkit.CRDVersion{Name: "v1alpha1", Served: true, Storage: true}))
	ratings := [][]int64{{7, 31, 77, 12, 3}, {2, 31, 77, 12, 3}, {0, 9, 40, 18}, {0, 0, 6, 2}, {0, 0, 0, 5}, {}, {}}
	n := 0
	for _, p := range fixturePodSpecs() {
		obj := fixturePodObject(p)
		src.Set(testkit.PodsGVR, obj)
		if p.ownerKind != "ReplicaSet" {
			continue
		}
		counts := ratings[n%len(ratings)]
		eosl := n%len(ratings) == len(ratings)-1
		n++
		for _, field := range []string{"initContainers", "containers"} {
			for _, c := range model.NestedSlice(obj.Object, "spec", field) {
				name, image := model.NestedString(c, "name"), model.NestedString(c, "image")
				cs := counts
				if field == "initContainers" {
					cs = nil // the init container's image clean: the pod's worst is its containers'
				}
				r := trivyFixtureReport(p.ns, p.ownerName, name, image, cs...)
				if eosl {
					_ = unstructured.SetNestedMap(r.Object, map[string]any{"family": "alpine", "name": "3.20.10", "eosl": true}, "report", "os")
				}
				src.Set(renderVulnGVR, r)
			}
		}
	}
	return src
}

func renderTrivyPods(t *testing.T, mode ThemeMode, name string) {
	conn := enginekit.MemoryConnector{Context: "kind-trivy", Src: trivyPodsFixture(), Profiles: kinds.Builtin(), CustomResources: true}
	s := engine.New(engine.Options{Connector: conn, SnapshotInterval: 5 * time.Millisecond,
		ClusterSettings: engine.NewClusterSettingsStore("")})
	t.Cleanup(s.Close)
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	// The cluster's integrations chosen (all on): the question about a new cluster's integrations
	// (D-76) does not cover the list.
	if _, err := s.ChangeClusterSettings("kind-trivy", func(cs *engine.ClusterSettings) error {
		cs.SetIntegrations(0, engine.IntAll, 0)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	sh, w, q := newEngineShell(t, s, Options{AutoConnect: "kind-trivy", Theme: mode})
	q.until(t, "the connection", func() bool { return sh.ui.Connection.State == engine.Connected && len(sh.kinds) > 0 })
	mustDispatch(t, s, "ui.openView", map[string]any{"view": "pods"})
	q.until(t, "the pods with their grades", func() bool {
		snap := sh.view.snap
		if snap == nil || snap.View != "pods" || !snap.Synced {
			return false
		}
		ci := slices.IndexFunc(snap.Columns, func(c model.Column) bool { return c.ID == "imageRating" })
		rated := 0
		for _, r := range snap.Rows {
			if ci >= 0 && ci < len(r.Cells) && r.Cells[ci].Grade > model.GradeNA {
				rated++
			}
		}
		return rated > 20
	})
	snap := sh.view.snap
	ci := slices.IndexFunc(snap.Columns, func(c model.Column) bool { return c.ID == "imageRating" })
	order := snap.Order
	if order == nil {
		t.Fatal("the column is not placed: no order")
	}
	at := slices.Index(order, ci)
	if at < 1 || snap.Columns[order[at-1]].ID != "containers" {
		t.Errorf("Image Rating shows at %d of %v, not after Containers", at, order)
	}
	seen := map[model.ImageGrade]bool{}
	for _, r := range snap.Rows {
		seen[r.Cells[ci].Grade] = true
	}
	for g := model.GradeNA; g <= model.GradeE; g++ {
		if !seen[g] {
			t.Errorf("no pod of grade %v", g)
		}
	}
	shields := 0
	for _, c := range findAll[*tableCell](sh.view.table) {
		if c.badge != nil && c.badge.Visible() {
			shields++
		}
	}
	if shields < 20 {
		t.Errorf("%d shields drawn", shields)
	}
	settle(w)
	savePNG(t, w, name)
}

func TestRenderTrivyPodsDark(t *testing.T)  { renderTrivyPods(t, ThemeDark, "trivy_pods_dark.png") }
func TestRenderTrivyPodsLight(t *testing.T) { renderTrivyPods(t, ThemeLight, "trivy_pods_light.png") }
