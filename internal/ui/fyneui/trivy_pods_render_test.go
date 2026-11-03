package fyneui

import (
	"slices"
	"testing"
	"time"

	"fyne.io/fyne/v2"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
	"github.com/guided-traffic/cogpit/internal/testkit"
	"github.com/guided-traffic/cogpit/internal/testkit/enginekit"
)

// The Pods list's image grades (D-78's second phase, D-113's grades) in the Signals column (D-150),
// over the real engine with the synthetic pods of the list renders and Trivy Operator's reports of
// most of their ReplicaSets: each pod's worst image grade as a shield in the column's second slot,
// after the pod's own state's, S to E; the pods of a Job, a DaemonSet and a node have no report and
// are N/A. PNGs: trivy_pods_{dark,light}.png.

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

// trivySignal is a row's Trivy signal in the Signals column ci.
func trivySignal(r model.Row, ci int) (model.Signal, bool) {
	if ci < 0 || ci >= len(r.Cells) || r.Cells[ci].Signals == nil {
		return model.Signal{}, false
	}
	for _, s := range *r.Cells[ci].Signals {
		if s.Source == model.SignalTrivy {
			return s, true
		}
	}
	return model.Signal{}, false
}

// trivyPodsShell opens the Pods list of trivyPodsFixture and waits for the grades.
func trivyPodsShell(t *testing.T, mode ThemeMode) (*shell, fyne.Window, *mainQueue) {
	conn := enginekit.MemoryConnector{Context: "kind-trivy", Src: trivyPodsFixture(), Profiles: kinds.Builtin(), CustomResources: true}
	s := newTestEngine(engine.Options{Connector: conn, SnapshotInterval: 5 * time.Millisecond,
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
		ci := slices.IndexFunc(snap.Columns, func(c model.Column) bool { return c.ID == model.SignalsColumnID })
		rated := 0
		for _, r := range snap.Rows {
			if s, ok := trivySignal(r, ci); ok && s.Grade > model.GradeNA {
				rated++
			}
		}
		return rated > 20
	})
	return sh, w, q
}

func renderTrivyPods(t *testing.T, mode ThemeMode, name string) {
	sh, w, _ := trivyPodsShell(t, mode)
	snap := sh.view.snap
	ci := slices.IndexFunc(snap.Columns, func(c model.Column) bool { return c.ID == model.SignalsColumnID })
	if ci != 1 || snap.Columns[ci].Slots != 2 {
		t.Errorf("the Signals column at %d: %+v", ci, snap.Columns[ci])
	}
	seen := map[model.ImageGrade]bool{}
	for _, r := range snap.Rows {
		if s, ok := trivySignal(r, ci); ok {
			seen[s.Grade] = true
			if s.Slot != 1 {
				t.Errorf("%s: the grade in slot %d", r.Ref.Name, s.Slot)
			}
		}
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

// A Signals cell draws the state's triangle and the grade's shield in their slots; the pointer on
// the shield shows the grade's text and counts, on the triangle the state's reason; a click on the
// shield asks the drawer for the section of the container that runs the worst image.
func TestSignalsCellDraws(t *testing.T) {
	sh, w, q := trivyPodsShell(t, ThemeDark)
	// Without a Status column no row is lit: the state's triangle shows beside the shield.
	sh.dispatch("ui.setColumns", map[string]any{"hidden": []string{"status"}}, nil)
	q.until(t, "the Status column hidden", func() bool {
		tv := sh.view
		return tv.snap != nil && !slices.ContainsFunc(tv.cols, func(ci int) bool { return tv.snap.Columns[ci].ID == columnStatus })
	})
	settle(w)
	var cell *tableCell
	q.until(t, "a cell with a triangle and a shield", func() bool {
		for _, c := range findAll[*tableCell](sh.view.table) {
			if c.content.kind != cellSignals || c.badge == nil || !c.badge.Visible() || c.content.lit {
				continue
			}
			if shown := c.shownSignals(); len(shown) == 2 && shown[0].Source == model.SignalState {
				cell = c
				return true
			}
		}
		return false
	})
	th := cell.Theme()
	step := signalStep(th)
	pad := th.Size(sizeCellPadding)
	if len(cell.sigIcons) == 0 || !cell.sigIcons[0].Visible() {
		t.Fatal("no triangle drawn")
	}
	if x := cell.badge.Position().X; x < pad+step-1 || x > pad+step+1 {
		t.Errorf("the shield at x %v, want slot 1 at %v", x, pad+step)
	}
	state := cell.signalAt(fyne.NewPos(pad+2, 10))
	grade := cell.signalAt(fyne.NewPos(pad+step+4, 10))
	if state == nil || state.Source != model.SignalState || grade == nil || grade.Source != model.SignalTrivy {
		t.Fatalf("at the slots: %+v, %+v", state, grade)
	}
	cell.sigHover = 1
	if tip := cell.tipText(); tip != grade.Text+"\n"+grade.Tip {
		t.Errorf("the shield's tooltip %q", tip)
	}
	cell.sigHover = 0
	if tip := cell.tipText(); tip != state.Text {
		t.Errorf("the triangle's tooltip %q", tip)
	}
	ref := sh.view.snap.Rows[cell.row].Ref
	sh.view.signalTapped(cell, fyne.NewPos(pad+step+4, 10))
	if want := sh.drawer.want; want == nil || !want.ref.Same(ref) || want.section != grade.Section || grade.Section == "" {
		t.Errorf("the drawer asked for %+v, want %s's %q", want, ref.Name, grade.Section)
	}
	if w := signalsWidth(th, sh.view.snap.Columns[1]); w < pad*2+2*step-signalGap {
		t.Errorf("the column's width %v holds no two slots", w)
	}
	// A column dragged narrower than its slots hides the icon that would reach past it.
	full := cell.Size()
	cell.Resize(fyne.NewSize(pad+step, full.Height))
	cell.layoutSignals(th, cell.Size())
	if cell.badge.Visible() || !cell.sigIcons[0].Visible() {
		t.Errorf("narrow: the shield shows %v, the triangle %v", cell.badge.Visible(), cell.sigIcons[0].Visible())
	}
	cell.Resize(full)
	cell.layoutSignals(th, cell.Size())
	if !cell.badge.Visible() {
		t.Error("wide again: the shield stays hidden")
	}
}
