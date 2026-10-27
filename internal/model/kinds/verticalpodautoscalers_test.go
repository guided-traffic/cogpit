package kinds_test

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	utiljson "k8s.io/apimachinery/pkg/util/json"

	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
)

const vpaKey = "verticalpodautoscalers.autoscaling.k8s.io"

// curatedProfile returns the curated profile of a custom resource kind key.
func curatedProfile(t testing.TB, key string) *model.KindProfile {
	t.Helper()
	for _, p := range kinds.Curated() {
		if p.Key == key {
			return p
		}
	}
	t.Fatalf("no curated profile %s", key)
	return nil
}

func FuzzVerticalPodAutoscalers(f *testing.F) {
	p := curatedProfile(f, vpaKey)
	for _, c := range loadCases(f, p.Key) {
		f.Add(c.json)
	}
	f.Add([]byte(`{}`))
	f.Add([]byte(`{"spec":{"targetRef":7},"status":{"recommendation":{"containerRecommendations":[{"target":{"cpu":"1e40"}}]}}}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		for _, decode := range []func([]byte, any) error{utiljson.Unmarshal, json.Unmarshal} {
			var m map[string]any
			if decode(data, &m) != nil || m == nil {
				continue
			}
			if err := exercise(p, &unstructured.Unstructured{Object: m}); err != nil {
				t.Fatal(err)
			}
		}
	})
}

// TestCuratedGolden renders every recorded object of the curated profiles, as TestGolden does for
// the built-in ones; TestCuratedTypeConfusion feeds them hostile values.
func TestCuratedGolden(t *testing.T) {
	for _, p := range kinds.Curated() {
		for _, c := range loadCases(t, p.Key) {
			t.Run(p.Key+"/"+c.name, func(t *testing.T) {
				checkGolden(t, strings.TrimSuffix(c.path, ".yaml")+".golden.json", encode(t, render(t, p, c.obj)))
			})
		}
	}
}

func TestCuratedTypeConfusion(t *testing.T) {
	if raceEnabled {
		t.Skip("one goroutine: the build without the race detector runs it (make test-norace-coverage)")
	}
	for _, p := range kinds.Curated() {
		for _, c := range loadCases(t, p.Key) {
			confuse(t, p.Key+"/"+c.name, c.obj, func(path []any, v any) {
				if err := exercise(p, c.obj); err != nil {
					t.Errorf("%s/%s with %v = %#v: %v", p.Key, c.name, path, v, err)
				}
			})
		}
	}
}

// TestCuratedProfiles checks what a curated profile must have: a custom kind's key, never a
// built-in one, the name first, lowerCamelCase ids, cells and a drawer.
func TestCuratedProfiles(t *testing.T) {
	for _, p := range kinds.Curated() {
		if p.Key != p.Resource+"."+p.Group || !strings.Contains(p.Group, ".") {
			t.Errorf("%s: key must be <plural>.<group>", p.Key)
		}
		if _, builtin := kinds.ByKey(p.Key); builtin {
			t.Errorf("%s: listed as a built-in kind", p.Key)
		}
		if p.Cells == nil || p.Detail == nil || p.Title == "" || len(p.Columns) == 0 || p.Columns[0].ID != "name" {
			t.Errorf("%s: incomplete profile", p.Key)
		}
		for _, c := range p.Columns {
			if !lowerCamel.MatchString(c.ID) {
				t.Errorf("%s: column id %q", p.Key, c.ID)
			}
		}
	}
}

// TestVerticalPodAutoscalerColumns pins the default columns of Lens's documentation (read
// 2026-10-13, not captured) and Cogpit's hidden ones after them.
func TestVerticalPodAutoscalerColumns(t *testing.T) {
	p := curatedProfile(t, vpaKey)
	var got []string
	for _, c := range p.Columns {
		title := c.Title
		if c.Hidden {
			title = "(" + title + ")"
		}
		got = append(got, title)
	}
	if s := strings.Join(got, ", "); s != "Name, Namespace, Mode, Age, Status, (Target), (CPU), (Memory)" {
		t.Errorf("columns %s", s)
	}
}

// TestProfileForVerticalPodAutoscalers: a cluster's VPA definition gets the curated profile at the
// version it serves (a copy at v1beta2, the shared profile untouched); a definition of another
// scope or kind, or of another kind key, gets the generic profile.
func TestProfileForVerticalPodAutoscalers(t *testing.T) {
	crd := recordedPath(t, filepath.Join("curated", "vpa-crd"))
	k, err := kinds.ParseCRD(crd)
	if err != nil {
		t.Fatal(err)
	}
	shared := curatedProfile(t, vpaKey)
	if p := kinds.ProfileFor(k); p != shared {
		t.Errorf("v1 served: profile %+v, want the curated one", p.Title)
	}
	beta := k
	beta.Version = "v1beta2"
	if p := kinds.ProfileFor(beta); p == shared || p.Title != "Vertical Pod Autoscalers" || p.Version != "v1beta2" {
		t.Errorf("v1beta2 served: %q at %q", p.Title, p.Version)
	}
	if shared.Version != "v1" {
		t.Errorf("the shared profile changed to %q", shared.Version)
	}
	alpha := k
	alpha.Version = "v1alpha1"
	clusterScoped := k
	clusterScoped.Namespaced = false
	other := k
	other.Group = "autoscaling.example.com"
	for name, kk := range map[string]kinds.CustomKind{"unknown version": alpha, "cluster-scoped": clusterScoped, "other group": other} {
		if p := kinds.ProfileFor(kk); p == shared || p.Title != "VerticalPodAutoscaler" {
			t.Errorf("%s: got %q, want the generic profile", name, p.Title)
		}
	}
}

// recordedPath reads testdata/<path>.yaml as the cache decodes it.
func recordedPath(t testing.TB, path string) *unstructured.Unstructured {
	t.Helper()
	dir, name := filepath.Split(path)
	for _, c := range loadCases(t, filepath.Clean(dir)) {
		if c.name == name {
			return c.obj
		}
	}
	t.Fatalf("no testdata %s", path)
	return nil
}

func TestVerticalPodAutoscalersStatusTexts(t *testing.T) {
	p := curatedProfile(t, vpaKey)
	const (
		none    = model.StatusNone
		ok      = model.StatusOK
		pending = model.StatusPending
		warning = model.StatusWarning
		bad     = model.StatusError
	)
	tests := []statusText{
		{vpaKey, "healthy", "mode", "Recreate", none, ok},
		{vpaKey, "minimal", "mode", "Auto", none, pending}, // the API's default when unset (inferred)
		{vpaKey, "terminating", "mode", "Off", none, model.StatusTerminating},
		{vpaKey, "healthy", "status", "RecommendationProvided", ok, ok},
		{vpaKey, "failing", "status", "ConfigUnsupported", bad, bad},
		{vpaKey, "no-pods", "status", "NoPodsMatched RecommendationProvided", warning, warning},
		{vpaKey, "low-confidence", "status", "LowConfidence RecommendationProvided", pending, pending},
		{vpaKey, "minimal", "status", "", pending, pending},
		{vpaKey, "healthy", "target", "Deployment/web", none, ok},
		// The target recommendation summed over the containers: 587m + 25m, 262144k + 64Mi.
		{vpaKey, "healthy", "cpuTarget", "0.612", none, ok},
		{vpaKey, "healthy", "memoryTarget", "314.0 MiB", none, ok},
		{vpaKey, "minimal", "cpuTarget", "-", none, pending},
	}
	objects := map[string]*unstructured.Unstructured{}
	for _, c := range loadCases(t, vpaKey) {
		objects[c.name] = c.obj
	}
	for _, tt := range tests {
		t.Run(tt.name+"/"+tt.column, func(t *testing.T) {
			i := p.ColumnIndex(tt.column)
			if i < 0 {
				t.Fatalf("no column %q", tt.column)
			}
			row := model.BuildRow(p, "test", objects[tt.name])
			cell := row.Cells[i]
			if text := model.CellText(p.Columns[i], cell, now); text != tt.text || cell.Status != tt.cell || row.Status != tt.row {
				t.Errorf("got %q (cell %q, row %q), want %q (cell %q, row %q)", text, cell.Status, row.Status, tt.text, tt.cell, tt.row)
			}
		})
	}
	row := model.BuildRow(p, "test", objects["healthy"])
	if l := row.Cells[p.ColumnIndex("target")].Link; l == nil || *l != (model.ObjectRef{Kind: "deployments", Namespace: "shop", Name: "web"}) {
		t.Errorf("target link %+v", l)
	}
}
