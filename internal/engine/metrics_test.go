package engine

import (
	"context"
	"slices"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"

	"github.com/guided-traffic/cogpit/internal/metrics"
	"github.com/guided-traffic/cogpit/internal/model"
)

// onePodProvider measures "web/a" only.
type onePodProvider struct{}

func (onePodProvider) ID() string                         { return "t" }
func (onePodProvider) Label() string                      { return "T" }
func (onePodProvider) Capabilities() metrics.Capabilities { return metrics.Capabilities{} }
func (onePodProvider) ListPods(context.Context, string) ([]model.PodUsage, error) {
	return []model.PodUsage{{Namespace: "web", Name: "a", Timestamp: time.Now(), Containers: []model.ContainerUsage{
		{Name: "app", Usage: model.Usage{CPUMilli: 40, MemoryBytes: 1 << 20}},
	}}}, nil
}
func (onePodProvider) GetPod(context.Context, string, string) (model.PodUsage, error) {
	return model.PodUsage{}, metrics.ErrNoData
}
func (onePodProvider) ListNodes(context.Context) ([]model.NodeUsage, error) { return nil, nil }
func (onePodProvider) GetNode(context.Context, string) (model.NodeUsage, error) {
	return model.NodeUsage{}, metrics.ErrNoData
}

func usagePods() *model.KindProfile {
	p := *testPods
	p.Columns = append(append([]model.Column(nil), testPods.Columns...),
		model.Column{ID: model.ColumnCPU, Title: "CPU", Type: model.ColumnMetric},
		model.Column{ID: model.ColumnMemory, Title: "Memory", Type: model.ColumnMetric})
	base := testPods.Cells
	p.Cells = func(obj *unstructured.Unstructured) ([]model.Cell, model.Status) {
		cells, st := base(obj)
		return append(cells, model.Cell{}, model.Cell{}), st
	}
	p.Usage = model.UsagePods
	p.UsageCells = func(_ *unstructured.Unstructured, u model.Usage) (model.Cell, model.Cell) {
		return model.Cell{Text: model.FormatCPU(u.CPUMilli), Num: u.CPUMilli}, model.Cell{Text: model.FormatBytes(u.MemoryBytes), Num: u.MemoryBytes}
	}
	return &p
}

func podObject(ns, name string) *unstructured.Unstructured {
	o := &unstructured.Unstructured{Object: map[string]any{}}
	o.SetNamespace(ns)
	o.SetName(name)
	o.SetUID(types.UID("uid-" + name))
	o.SetResourceVersion("1")
	return o
}

// Rows show a measurement when the feed has one, stay as the profile built them otherwise, and
// fall back to the profile's row when the feed goes away; the patched row is what sorts.
func TestRowCachePatchesUsageAndFallsBack(t *testing.T) {
	p := usagePods()
	hub := metrics.NewHub(metrics.Fixed(onePodProvider{}), metrics.Options{ListInterval: time.Hour})
	defer hub.Close()
	b := &Backend{Metrics: hub}
	signalled := make(chan struct{}, 4)
	feed := b.startUsageFeed(p, nsPlan{}, func() { signalled <- struct{}{} })
	if feed == nil {
		t.Fatal("no feed for a kind with Usage")
	}
	select {
	case <-signalled:
	case <-time.After(5 * time.Second):
		t.Fatal("the feed never polled")
	}

	objs := []*unstructured.Unstructured{podObject("web", "a"), podObject("web", "b")}
	rc := &rowCache{}
	cpu := func(s *TableSnapshot, name string) model.Cell {
		for i := range s.Rows {
			if s.Rows[i].Ref.Name == name {
				return cellAt(&s.Rows[i], p.ColumnIndex(model.ColumnCPU))
			}
		}
		t.Fatalf("no row %s", name)
		return model.Cell{}
	}
	params := viewParams{sort: SortSpec{Column: model.ColumnCPU, Desc: true}}
	snap := rc.buildWith(p, "ctx", objs, params, nil, 0, feed, nil)
	if c := cpu(snap, "a"); c.Text != "40m" || c.Num != 40 {
		t.Errorf("a: cpu %+v, want its measurement", c)
	}
	if c := cpu(snap, "b"); c.Text != "" {
		t.Errorf("b has no measurement but shows %+v", c)
	}
	if snap.Rows[0].Ref.Name != "a" {
		t.Errorf("sorted by CPU descending: %s first, want the pod with a measurement", snap.Rows[0].Ref.Name)
	}
	// Ascending: the empty cell still sorts last.
	params.sort.Desc = false
	if snap = rc.buildWith(p, "ctx", objs, params, nil, 0, feed, nil); snap.Rows[0].Ref.Name != "a" || snap.Rows[1].Ref.Name != "b" {
		t.Errorf("ascending: %s, %s; pods without a measurement sort last in both directions", snap.Rows[0].Ref.Name, snap.Rows[1].Ref.Name)
	}

	// The profile's rows are untouched: a build without the feed shows no measurement.
	if c := cpu(rc.build(p, "ctx", objs, params, nil, 0), "a"); c.Text != "" {
		t.Errorf("without the feed a shows %+v, want the profile's empty cell", c)
	}
	// A kind without usage columns gets no feed.
	if f := b.startUsageFeed(testServices, nsPlan{}, func() {}); f != nil {
		t.Error("a feed for a kind without Usage")
	}
	feed.release()
	if hub.Subscribed() != 0 {
		t.Error("the feed's poll outlived its release")
	}
	if (*usageFeed)(nil).generation() != 0 {
		t.Error("a nil feed has a generation")
	}
}

// A profile whose UsageCells panics yields the profile's row, not a crash.
func TestWithUsageSurvivesAPanickingProfile(t *testing.T) {
	p := usagePods()
	p.UsageCells = func(*unstructured.Unstructured, model.Usage) (model.Cell, model.Cell) { panic("bug") }
	row := model.Row{Cells: make([]model.Cell, len(p.Columns))}
	got := withUsage(p, row, podObject("a", "b"), model.Usage{CPUMilli: 1}, 6, 7)
	if len(got.Cells) != len(row.Cells) {
		t.Error("the row changed shape")
	}
}

// servicesOf reads the numeric forms a cached Service's port arrives in (JSON decodes to int64 or
// float64) and skips a port outside 1–65535, which would otherwise wrap into another port's number
// (1<<32 + 9090 read as 9090).
func TestServicesOfPorts(t *testing.T) {
	svc := &unstructured.Unstructured{Object: map[string]any{
		"metadata": map[string]any{"namespace": "monitoring", "name": "prometheus-server"},
		"spec": map[string]any{"ports": []any{
			map[string]any{"name": "a", "port": int64(80)},
			map[string]any{"name": "b", "port": float64(9090)},
			map[string]any{"name": "c", "port": int64(65535)},
			map[string]any{"name": "wrapped", "port": int64(1<<32 + 9090)},
			map[string]any{"name": "large", "port": int64(65536)},
			map[string]any{"name": "zero", "port": int64(0)},
			map[string]any{"name": "negative", "port": float64(-1)},
			map[string]any{"name": "missing"},
			"not a port",
		}},
	}}
	got := servicesOf([]*unstructured.Unstructured{svc})
	want := []metrics.ServicePort{{Name: "a", Port: 80}, {Name: "b", Port: 9090}, {Name: "c", Port: 65535}}
	if len(got) != 1 || got[0].Namespace != "monitoring" || got[0].Name != "prometheus-server" || !slices.Equal(got[0].Ports, want) {
		t.Fatalf("servicesOf = %+v, want monitoring/prometheus-server with %+v", got, want)
	}
}
