package commands_test

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// A repository whose name holds a dot is used (A36, owner): its charts' ids double the dot
// ("charts..example.web"), and the id reaches the chart through every path that takes one: the
// object, ui.navigate and the drawer (an agent's, and a cogpit:// link's name),
// helm.chart.installTab and an install from its tab. A repository without a dot keeps the ids it
// always had, and an id read the old way ("charts.example.web") names no chart of the dotted one.
func TestHelmRepositoryWithADot(t *testing.T) {
	src := testkit.NewMemorySource()
	fi := newFakeInstaller(t, src)
	h := newChartHarnessWith(t, engine.NewMemoryHelmJournal(), src, fi)
	h.helmFile.Write(h.repo.Entry("test"), h.repo.Entry("charts.example"))
	res, err := h.dispatch(engine.OriginGUI, "helm.repo.update", struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	if infos := res.([]engine.HelmRepoInfo); len(infos) != 2 || infos[1].Name != "charts.example" || infos[1].Unused || infos[1].Error != "" || infos[1].Charts != 1 {
		t.Fatalf("updated %+v", infos)
	}
	if names := h.charts(t, 2); !slices.Equal(names, []string{"charts..example.web", "test.web"}) {
		t.Fatalf("charts %v", names)
	}
	dotted := engine.HelmChartRef("test", "charts.example", "web")
	if dotted.Name != "charts..example.web" {
		t.Fatalf("the dotted repository's chart ref %+v", dotted)
	}
	obj, err := h.s.Lookup(dotted)
	if err != nil {
		t.Fatal(err)
	}
	if spec, _ := obj.Object["spec"].(map[string]any); spec["repository"] != "charts.example" || spec["name"] != "web" {
		t.Errorf("the dotted repository's chart %v", spec)
	}
	if _, err := h.s.Lookup(model.ObjectRef{Context: "test", Kind: kinds.HelmChartsKey, Name: "test.web"}); err != nil {
		t.Errorf("the id of a repository without a dot, as it always was: %v", err)
	}

	// An agent shows the chart (as a cogpit:// link does, through ui.navigate) and reads its drawer.
	if _, err := h.dispatch(engine.OriginExternalAgent, "ui.navigate", map[string]any{"ref": dotted}); err != nil {
		t.Fatal(err)
	}
	if d := h.s.UIState().Drawer; d == nil || d.Name != dotted.Name || d.Kind != kinds.HelmChartsKey {
		t.Errorf("the drawer shows %+v", d)
	}
	got, err := h.dispatch(engine.OriginExternalAgent, "object.get", map[string]any{"ref": dotted})
	if err != nil {
		t.Fatal(err)
	}
	if js, _ := json.Marshal(got); !strings.Contains(string(js), `"helmChart":{"repository":"charts.example","chart":"web"`) {
		t.Errorf("object_get of the dotted repository's chart: %s", js)
	}
	link, err := engine.ParseDeepLink("cogpit://open?view=helmcharts&name=charts..example.web")
	if err != nil || link.Name != dotted.Name || link.View != kinds.HelmChartsKey {
		t.Errorf("a cogpit:// link to the chart: %+v %v", link, err)
	}

	// The id read the old way, at its first dot, names repository "charts" and chart "example.web":
	// no such chart.
	if _, err := h.dispatch(engine.OriginGUI, "helm.chart.installTab", map[string]any{"ref": model.ObjectRef{Context: "test", Kind: kinds.HelmChartsKey, Name: "charts.example.web"}}); err == nil {
		t.Error("an id read the old way opened a tab")
	}

	// Install opens its tab on the dotted repository's chart, and installs from it.
	res, err = h.dispatch(engine.OriginGUI, "helm.chart.installTab", map[string]any{"ref": dotted})
	if err != nil {
		t.Fatal(err)
	}
	tab := res.(commands.EditResult).Tab
	if hi := tab.HelmInstall; hi == nil || hi.Repository != "charts.example" || hi.Chart != "web" || tab.Ref == nil || tab.Ref.Name != dotted.Name {
		t.Fatalf("the install tab %+v", tab)
	}
	if _, err := h.dispatch(engine.OriginGUI, "helm.chart.install", map[string]any{"tab": tab.ID, "textRevision": h.tabRevision(t, tab.ID),
		"namespace": "apps", "name": "web"}); err != nil {
		t.Fatal(err)
	}
	if ins := fi.recordedInstalls(); len(ins) != 1 || ins[0].Chart == nil || ins[0].Chart.Repository != "charts.example" || ins[0].Chart.Name != "web" {
		t.Errorf("installed %+v", ins)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	toast := false
	for _, ev := range h.events {
		toast = toast || (ev.Type == engine.EventToast && strings.Contains(ev.Message, "from charts.example/web 0.2.0"))
	}
	if !toast {
		t.Error("the install's toast does not name charts.example/web")
	}
}
