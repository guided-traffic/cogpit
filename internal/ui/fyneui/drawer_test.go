package fyneui

import (
	"slices"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"github.com/guided-traffic/cogpit/internal/model"
)

// richPod is the fixture pod the drawer tests and PNGs show: crash-looping, with an init
// container, events and folded lists.
func richPod(t testing.TB) podSpec {
	t.Helper()
	for _, p := range fixturePodSpecs() {
		if p.state == podCrashLoop && len(p.init) > 0 {
			return p
		}
	}
	t.Fatal("no crash-looping fixture pod with an init container")
	return podSpec{}
}

func drawerTexts(sh *shell) []string {
	var out []string
	for _, o := range findAll[fyne.CanvasObject](sh.drawer.details) {
		switch x := o.(type) {
		case *textLine:
			out = append(out, x.text)
		case *sourceLine: // the metrics' caption (metricssource.go)
			if x.Visible() {
				out = append(out, x.text)
			}
		case *sourceChip: // a provider on the metrics' source row (metricsstatus.go)
			if x.Visible() {
				out = append(out, x.text())
			}
		case *paragraph: // a name wider than its column, wrapped (drawer.go itemName)
			out = append(out, x.text)
		case *tableSortHeader: // a sortable table's column title (drawertable_sort.go)
			out = append(out, x.title)
		}
	}
	return out
}

func drawerLinks(sh *shell) map[string]*linkText {
	out := map[string]*linkText{}
	for _, l := range findAll[*linkText](sh.drawer.details) {
		out[l.text] = l
	}
	return out
}

// TestDrawerSectionsAsLens: Properties first (no UID), the kind's sections after it with the
// containers under their group headings, the events last; labels fold behind their count and
// unfold on click; values that name objects are links.
func TestDrawerSectionsAsLens(t *testing.T) {
	f := kindsFake(t, "pods")
	p := richPod(t)
	ref := model.ObjectRef{Context: "demo", Kind: "pods", Namespace: p.ns, Name: p.name}
	f.ui.Selected, f.ui.Drawer = []model.ObjectRef{ref}, &ref
	sh, _, q := newTestShell(t, f, Options{})
	q.until(t, "the drawer", func() bool { return sh.drawer.last != nil && len(sh.drawer.details.Objects) > 2 })

	texts := drawerTexts(sh)
	// The Metrics section (metricsview.go) is the first of Lens's order; the rest follows it.
	if texts[0] != "Metrics" {
		t.Fatalf("Metrics must come first: %q", texts)
	}
	texts = texts[slices.Index(texts, "Properties"):]
	idx := func(s string) int { return slices.Index(texts, s) }
	order := []string{"Properties", "Pod Volumes", "Init Containers", "migrate", "Containers", "app", "Events"}
	for i := 1; i < len(order); i++ {
		if idx(order[i-1]) < 0 || idx(order[i]) < idx(order[i-1]) {
			t.Fatalf("%q must come before %q in %q", order[i-1], order[i], texts)
		}
	}
	if idx("Properties") != 0 || idx("UID") >= 0 || idx("Metadata") >= 0 {
		t.Errorf("Properties must come first, without UID or a Metadata section: %q", texts)
	}
	if idx("BackOff") < idx("Events") {
		t.Errorf("the events must follow the Events heading: %q", texts)
	}

	links := drawerLinks(sh)
	labels := links["2 Labels"]
	if labels == nil {
		t.Fatalf("no folded labels among the links %v", keys(links))
	}
	if slices.Contains(drawerTexts(sh), "No events found") {
		t.Error("a pod with events says none")
	}
	pills := func() int { return len(findAll[*pill](sh.drawer.details)) }
	before := pills()
	test.Tap(labels)
	if after := pills(); after != before+2 {
		t.Errorf("unfolding the labels added %d chips, want 2", after-before)
	}
	test.Tap(drawerLinks(sh)["2 Labels"])
	if pills() != before {
		t.Error("the labels did not fold again")
	}

	test.Tap(drawerLinks(sh)[p.node])
	q.until(t, "ui.navigate", func() bool { return len(f.callsOf("ui.navigate")) == 1 })
	if got := decodeParams[refParams](t, f.callsOf("ui.navigate")[0]).Ref; got != (model.ObjectRef{Context: "demo", Kind: "nodes", Name: p.node}) {
		t.Errorf("node link %+v", got)
	}
}

func TestDrawerNoEventsFound(t *testing.T) {
	f := kindsFake(t, "nodes")
	ref := model.ObjectRef{Context: "demo", Kind: "nodes", Name: "worker-a-01"}
	f.ui.Selected, f.ui.Drawer = []model.ObjectRef{ref}, &ref
	sh, _, q := newTestShell(t, f, Options{})
	q.until(t, "the drawer", func() bool { return sh.drawer.last != nil && len(sh.drawer.details.Objects) > 2 })
	texts := drawerTexts(sh)
	if texts[len(texts)-1] != "No events found" {
		t.Errorf("last line %q, want No events found", texts[len(texts)-1])
	}
	for _, want := range []string{"Capacity", "Allocatable", "CPU", "Ephemeral Storage", "Kubelet version"} {
		if !slices.Contains(texts, want) {
			t.Errorf("node drawer lacks %q: %q", want, strings.Join(texts, ", "))
		}
	}
	if sh.drawer.status.Visible() {
		t.Error("a node has no Status item; the drawer shows no pill")
	}
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}
