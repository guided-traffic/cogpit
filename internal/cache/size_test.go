package cache

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	toolscache "k8s.io/client-go/tools/cache"
	"sigs.k8s.io/yaml"
)

// fixtureDocs returns the built-in kinds' synthetic fixtures (model/kinds/testdata) as JSON, by kind.
func fixtureDocs(t *testing.T) map[string][][]byte {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("..", "model", "kinds", "testdata", "*", "*.yaml"))
	if err != nil || len(files) < 100 {
		t.Fatalf("fixtures: %d files, %v", len(files), err)
	}
	out := map[string][][]byte{}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		doc, err := yaml.YAMLToJSON(data)
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		kind := filepath.Base(filepath.Dir(f))
		out[kind] = append(out[kind], doc)
	}
	return out
}

// heapAfter returns the live heap after a collection.
func heapAfter() uint64 {
	runtime.GC()
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.HeapAlloc
}

// decodeStore decodes copies of each document, as client-go decodes a list or watch event, into an
// indexer with the namespace index, as an informer's store holds them, and returns the heap they
// take, the walk of every object (the model) and the memo's first estimate (the model, the objects
// beyond its walk at the mean).
func decodeStore(t *testing.T, docs [][]byte, copies int) (measured, walked, estimated int64, objects int) {
	t.Helper()
	store := toolscache.NewIndexer(toolscache.MetaNamespaceKeyFunc, toolscache.Indexers{toolscache.NamespaceIndex: toolscache.MetaNamespaceIndexFunc})
	before := heapAfter()
	for i := range copies {
		for _, doc := range docs {
			u := &unstructured.Unstructured{}
			if err := u.UnmarshalJSON(doc); err != nil {
				t.Fatal("an object does not decode") // never the object: the real cluster's test decodes here too
			}
			u.SetName(fmt.Sprintf("%s-%05d", u.GetName(), i)) // a key of its own, as distinct objects have
			if err := store.Add(u); err != nil {
				t.Fatal(err)
			}
		}
	}
	measured = int64(heapAfter() - before)
	objs := storeObjects(store.List())
	for _, u := range objs {
		walked += objectBytes(u)
	}
	var memo sizeMemo
	estimated, _ = memo.estimate(objs)
	runtime.KeepAlive(store)
	return measured, walked, estimated, len(objs)
}

// The estimate against the heap Go reports for the same objects in a store: every built-in kind's
// fixtures, decoded as client-go decodes them, hundreds of copies each so that the heap's own noise
// does not count. The model (every object walked, as the size memo does for a scope of up to 1,000
// objects) must be within ±10 % of the heap per kind and ±5 % over all of them. Measured on Go
// 1.27.2, darwin/arm64 (the log shows each kind's ratio): 0.93–0.99 per kind and 0.97 over all (it
// leaves out a few percent).
func TestMemoryEstimateAgainstTheHeap(t *testing.T) {
	if testing.Short() || raceEnabled {
		t.Skip("decodes every fixture many times; the race detector's heap is laid out otherwise")
	}
	byKind := fixtureDocs(t)
	kinds := make([]string, 0, len(byKind))
	for k := range byKind {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	var allMeasured, allWalked int64
	var lines []string
	for _, kind := range kinds {
		docs := byKind[kind]
		copies := max(900/len(docs), 1) // at most walkBudget objects: one read walks them all
		measured, walked, estimated, n := decodeStore(t, docs, copies)
		if measured <= 0 {
			t.Fatalf("%s: no heap measured", kind)
		}
		if estimated != walked {
			t.Errorf("%s: %d objects estimated at %d B, walked %d B: the memo did not walk them all", kind, n, estimated, walked)
		}
		model := float64(walked) / float64(measured)
		lines = append(lines, fmt.Sprintf("%-48s %5d objects, %6d B each on the heap, the model %.3f", kind, n, measured/int64(n), model))
		if model < 0.9 || model > 1.1 {
			t.Errorf("%s: the model %d B, the heap %d B (ratio %.2f)", kind, walked, measured, model)
		}
		allMeasured += measured
		allWalked += walked
	}
	model := float64(allWalked) / float64(allMeasured)
	t.Logf("memory estimate against the heap:\n%s\nall: %d B on the heap, the model %.3f", strings.Join(lines, "\n"), allMeasured, model)
	if model < 0.95 || model > 1.05 {
		t.Errorf("over all kinds: the model %.2f", model)
	}
}

// memoObject is an object of the size memo's tests.
func memoObject(i int, rv string, payload int) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"metadata": map[string]any{"name": fmt.Sprintf("o-%05d", i), "namespace": "a", "uid": fmt.Sprintf("uid-%05d", i), "resourceVersion": rv},
		"data":     map[string]any{"x": strings.Repeat("v", payload)},
	}}
}

// The size memo walks at most walkBudget objects per read and counts the rest at the mean, so a
// large scope's estimate converges over a few reads and is then the walk of every object; a read
// after that walks only an object whose resourceVersion changed, and objects that left are
// forgotten.
func TestSizeMemo(t *testing.T) {
	var objs []*unstructured.Unstructured
	var want int64
	for i := range 2500 {
		o := memoObject(i, "1", 100+i%7*100) // sizes that differ, so the mean is an estimate
		objs = append(objs, o)
		want += objectBytes(o)
	}
	var m sizeMemo
	for read, wantKnown := range []int{1000, 2000, 2500, 2500} {
		got, known := m.estimate(objs)
		if known != wantKnown {
			t.Errorf("read %d: %d sizes known, want %d", read+1, known, wantKnown)
		}
		if wantKnown == len(objs) && got != want {
			t.Errorf("read %d: %d B, want the walk of every object, %d B", read+1, got, want)
		}
		if diff := float64(got-want) / float64(want); diff < -0.2 || diff > 0.2 {
			t.Errorf("read %d: %d B, %.0f%% off the walk of every object", read+1, got, diff*100)
		}
	}
	bigger := memoObject(7, "2", 50_000)
	want += objectBytes(bigger) - objectBytes(objs[7])
	objs[7] = bigger
	if got, _ := m.estimate(objs); got != want {
		t.Errorf("after one object changed: %d B, want %d B", got, want)
	}
	objs = objs[:100]
	if _, known := m.estimate(objs); known != 100 || len(m.sizes) != 100 {
		t.Errorf("after 2,400 objects left: %d sizes known, %d remembered", known, len(m.sizes))
	}
}

// The size classes and maps as Go 1.24+ lays them out.
func TestAllocAndMapBytes(t *testing.T) {
	for n, want := range map[int64]int64{1: 8, 8: 8, 9: 16, 33: 48, 264: 288, 32768: 32768, 32769: 40960} {
		if got := alloc(n); got != want {
			t.Errorf("alloc(%d) = %d, want %d", n, got, want)
		}
	}
	for n, want := range map[int]int64{0: 48, 1: 48 + 288, 8: 48 + 288, 9: 48 + 8 + 32 + 576, 14: 48 + 8 + 32 + 576, 15: 48 + 8 + 32 + 1152} {
		if got := mapBytes(n); got != want {
			t.Errorf("mapBytes(%d) = %d, want %d", n, got, want)
		}
	}
}
