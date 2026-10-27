package engine

import (
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/testkit"
)

// benchPods returns n pods spread over 20 namespaces and 50 nodes, with varied restarts and ages,
// each with a resourceVersion as the cache would hold it.
func benchPods(n int) []*unstructured.Unstructured {
	out := make([]*unstructured.Unstructured, 0, n)
	base := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	for i := range n {
		p := testkit.Pod(fmt.Sprintf("team-%02d", i%20), fmt.Sprintf("web-%05d-%x", i, i*7919%4096))
		p.Object["spec"].(map[string]any)["nodeName"] = fmt.Sprintf("node-%02d", i%50)
		p.Object["status"].(map[string]any)["containerStatuses"].([]any)[0].(map[string]any)["restartCount"] = int64(i * 31 % 17)
		p.Object["metadata"].(map[string]any)["creationTimestamp"] = base.Add(time.Duration(i*7%10007) * time.Second).Format(time.RFC3339)
		p.SetResourceVersion(strconv.Itoa(i + 1))
		out = append(out, p)
	}
	return out
}

// BenchmarkSnapshot10k measures a snapshot of a 10,000-pod view from a warm row cache: build
// (row reuse by resourceVersion), filter and sort. Gate criterion S2, budget P2 (≤ 50 ms).
func BenchmarkSnapshot10k(b *testing.B) {
	objs := benchPods(10_000)
	cases := []struct {
		name   string
		params viewParams
	}{
		{"name", viewParams{sort: SortSpec{Column: "name"}}},
		{"namespaces", viewParams{sort: SortSpec{Column: "name"}, namespaces: []string{"team-01", "team-02", "team-03"}}},
		{"filter", viewParams{sort: SortSpec{Column: "age", Desc: true}, filter: "web-0 node-1"}},
	}
	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			rc := &rowCache{}
			rc.build(testPods, "bench", objs, c.params, nil, 0) // warm
			b.ReportAllocs()
			for b.Loop() {
				snap := rc.build(testPods, "bench", objs, c.params, nil, 0)
				if snap.Total != len(objs) {
					b.Fatal("short snapshot")
				}
			}
		})
	}
}

// BenchmarkSortChange10k measures re-sorting a 10,000-pod view by another column, from a warm
// row cache: what a click on a column header costs. Gate criterion S2, budget P2 (≤ 50 ms).
func BenchmarkSortChange10k(b *testing.B) {
	objs := benchPods(10_000)
	sorts := []SortSpec{
		{Column: "name"}, {Column: "age", Desc: true}, {Column: "restarts", Desc: true},
		{Column: "node"}, {Column: "namespace", Desc: true}, {Column: "status"},
	}
	rc := &rowCache{}
	rc.build(testPods, "bench", objs, viewParams{sort: sorts[0]}, nil, 0)
	b.ReportAllocs()
	i := 0
	for b.Loop() {
		i++
		rc.build(testPods, "bench", objs, viewParams{sort: sorts[i%len(sorts)]}, nil, 0)
	}
}

// BenchmarkSortCommand10k measures the whole path of a sort change on an idle 10,000-pod view:
// the UI state change, the worker's rebuild and the table event.
func BenchmarkSortCommand10k(b *testing.B) {
	src := testkit.NewMemorySource()
	for _, p := range benchPods(10_000) {
		src.Set(testkit.PodsGVR, p)
	}
	const interval = 100 * time.Millisecond
	s, _ := newTestSession(b, src, interval)
	connectTest(b, s)
	waitTable(b, s, "pods", func(t *TableSnapshot) bool { return t.Synced && len(t.Rows) == 10_000 })
	got := make(chan SortSpec, 16)
	s.Subscribe(func(ev Event) {
		if ev.Type == EventTable && ev.View == "pods" {
			if t := s.Table("pods"); t != nil {
				select {
				case got <- t.Sort:
				default: // never block the worker
				}
			}
		}
	})
	sorts := []SortSpec{{Column: "age", Desc: true}, {Column: "name"}}
	i := 0
	for b.Loop() {
		b.StopTimer()
		time.Sleep(interval) // an idle view: the previous snapshot is an interval old
		for len(got) > 0 {
			<-got
		}
		want := sorts[i%len(sorts)] // starts with a change: the view sorts by name
		i++
		b.StartTimer()
		s.UpdateUI(func(u *UIState) { u.Sort = want })
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		for done := false; !done; {
			select {
			case sp := <-got:
				done = sp == want
			case <-ctx.Done():
				b.Fatal("no snapshot with the new sort")
			}
		}
		cancel()
	}
}
