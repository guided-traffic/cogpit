package fyneui

import (
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2/widget"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// The pod drawer's image grades (D-78, D-113): a pod of a ReplicaSet with two containers and an init
// container, each container's section graded from Trivy Operator's reports as the engine builds them
// (engine.PodImageGrades): the shield after the container's name and the card under its items; app
// D, envoy C for its OS's end of life, and the init container's image not scanned, N/A.

// trivyFixtureReport is a report of one container of a fixture pod's ReplicaSet, shaped as Trivy
// Operator writes it (kind, 2026-10-17), the image as the fixture names it.
func trivyFixtureReport(ns, rs, container, image string, counts ...int64) *unstructured.Unstructured {
	counts = append(counts, 0, 0, 0, 0, 0)
	ref, tag, _ := strings.Cut(strings.TrimPrefix(image, "registry.example.com/"), ":")
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "aquasecurity.github.io/v1alpha1", "kind": "VulnerabilityReport",
		"metadata": map[string]any{
			"name": "replicaset-" + rs + "-" + container, "namespace": ns,
			"labels": map[string]any{
				model.TrivyLabelResourceKind: "ReplicaSet", model.TrivyLabelResourceName: rs,
				model.TrivyLabelResourceNamespace: ns, model.TrivyLabelContainerName: container,
			},
		},
		"report": map[string]any{
			"artifact": map[string]any{"repository": ref, "tag": tag},
			"registry": map[string]any{"server": "registry.example.com"},
			"summary": map[string]any{"criticalCount": counts[0], "highCount": counts[1], "mediumCount": counts[2],
				"lowCount": counts[3], "unknownCount": counts[4]},
			"updateTimestamp": fixedNow.Add(-3 * time.Hour).Format(time.RFC3339),
		},
	}}
}

// trivyPodFake is the pods view with the drawer open on a pod of two containers and an init
// container, its model carrying the image ratings.
func trivyPodFake(t *testing.T) (*engine.DetailModel, *fakeEngine) {
	f := kindsFake(t, "pods")
	registerProbes(t, f)
	// The reports' custom kind, which the cluster serves: the Report item links to its object.
	f.kinds = append(f.kinds, kindInfo{Kind: "vulnerabilityreports.aquasecurity.github.io", Title: "VulnerabilityReport",
		APIKind: "VulnerabilityReport", Group: "aquasecurity.github.io", Version: "v1alpha1", Namespaced: true})
	var pod podSpec
	for _, p := range fixturePodSpecs() {
		if len(p.containers) == 2 && len(p.init) == 1 && p.state == podOK && p.ownerKind == "ReplicaSet" {
			pod = p
			break
		}
	}
	if pod.name == "" {
		t.Fatal("no fixture pod of two containers and an init container")
	}
	obj := fixturePodObject(pod)
	ref := model.ObjectRef{Context: "demo", Kind: "pods", Namespace: pod.ns, Name: pod.name}
	image := func(field, name string) string {
		for _, c := range model.NestedSlice(obj.Object, "spec", field) {
			if model.NestedString(c, "name") == name {
				return model.NestedString(c, "image")
			}
		}
		t.Fatalf("no container %s", name)
		return ""
	}
	envoy := trivyFixtureReport(pod.ns, pod.ownerName, "envoy", image("containers", "envoy"))
	_ = unstructured.SetNestedMap(envoy.Object, map[string]any{"family": "alpine", "name": "3.20.10", "eosl": true}, "report", "os")
	reports := []*unstructured.Unstructured{
		trivyFixtureReport(pod.ns, pod.ownerName, "app", image("containers", "app"), 3, 12, 30, 8, 2),
		envoy,
	}
	m := f.details[ref.String()]
	m.Sections = engine.PodImageGrades(obj, reports, m.Sections, fixedNow)
	f.ui.Selected, f.ui.Drawer = []model.ObjectRef{ref}, &ref
	return m, f
}

func renderImageGrades(t *testing.T, mode ThemeMode, name string) {
	m, f := trivyPodFake(t)
	grades := map[string]model.ImageGrade{}
	for _, sec := range m.Sections {
		if sec.Grade != nil {
			grades[sec.Title] = sec.Grade.Grade
		}
	}
	if len(grades) != 3 || grades["app"] != model.GradeD || grades["envoy"] != model.GradeC || grades["migrate"] != model.GradeNA {
		t.Fatalf("graded containers %v", grades)
	}
	sh, w, q := newTestShell(t, f, Options{Theme: mode})
	q.until(t, "the drawer", func() bool {
		return len(sh.kinds) > 0 && sh.drawer.last != nil && len(sh.drawer.actions.Objects) > 0 && sh.diag.total > 0
	})
	settle(w)
	all := drawerTexts(sh)
	for _, p := range findAll[*pill](sh.drawer.details) { // a value with a status: its pill
		all = append(all, p.text)
	}
	for _, rt := range findAll[*widget.RichText](sh.drawer.details) { // the grade cards' texts
		all = append(all, rt.String())
	}
	texts := strings.Join(all, "\n")
	for _, want := range []string{"D · 3 Critical", "C · OS end of life", "N/A · not scanned", "app", "envoy", "migrate"} {
		if !strings.Contains(texts, want) {
			t.Errorf("the drawer does not show %q", want)
		}
	}
	if strings.Contains(texts, "Image Ratings") {
		t.Error("the grades have a section of their own")
	}
	if n := len(findAll[*gradeBadge](sh.drawer.details)); n != 6 { // a shield after each name and on each card
		t.Errorf("%d shields in the drawer", n)
	}
	if n := len(findAll[*severityBar](sh.drawer.details)); n != 2 { // N/A has no counts
		t.Errorf("%d severity bars", n)
	}
	scrollDrawerTo(t, sh, "app")
	settle(w)
	savePNG(t, w, name)
}

func TestRenderPodImageGradesDark(t *testing.T) {
	renderImageGrades(t, ThemeDark, "pod_image_grades_dark.png")
}

func TestRenderPodImageGradesLight(t *testing.T) {
	renderImageGrades(t, ThemeLight, "pod_image_grades_light.png")
}
