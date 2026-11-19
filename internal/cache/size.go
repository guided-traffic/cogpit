package cache

import (
	"math/bits"
	"sync"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Memory per kind (G3, the Diagnostics view): an estimate of the heap the cache's stored objects
// take, cheap enough to compute whenever the Diagnostics tab or an agent reads it (once a second at
// most) without holding up the informers. Each object is walked as client-go decoded it into an
// unstructured map, adding up the allocations Go makes for it (sizeOf: maps as Go 1.24+ lays them
// out, strings, slices, boxed numbers, each rounded to the allocator's size classes), plus the
// store's own entry for it (storeEntryBytes and its key). A scope remembers each object's size by
// its UID and resourceVersion (sizeMemo), so a read walks only the objects that changed since the
// last one, at most walkBudget of them; an object not walked yet counts at the mean of those that
// were, and the next read walks it. Once every object was walked, the estimate is the model's for
// every object, and a read costs a lookup per object.
//
// Its error, against the heap runtime.ReadMemStats reports for the same objects decoded into a store
// (TestMemoryEstimateAgainstTheHeap, every built-in kind's fixtures, Go 1.27): 0.93–0.99 per kind,
// 0.97 over all; on a real cluster's objects of seven kinds (TestRealMemoryEstimateAgainstTheHeap)
// 0.96–1.00 per kind, 0.99 over all: it leaves out a few percent. Not counted: what client-go's
// informer holds besides its store (its queue between a list and the store, the reflector's
// buffers), the label-selected views' own indexes, the size memo itself, and the table snapshots
// and row caches the engine builds from the objects.

// walkBudget bounds the objects one read walks per scope; the rest count at the walked ones' mean.
const walkBudget = 1000

// storeEntryBytes is a store's own memory per object besides its key's bytes: the items map's slot
// (the key's header and the object's interface) and the namespace index's set entry, at a typical
// load of Go's maps.
const storeEntryBytes = 96

// sizeClasses are Go's malloc size classes up to 32 KiB (internal/runtime/gc/sizeclasses.go).
var sizeClasses = []int64{8, 16, 24, 32, 48, 64, 80, 96, 112, 128, 144, 160, 176, 192, 208, 224, 240, 256, 288, 320,
	352, 384, 416, 448, 480, 512, 576, 640, 704, 768, 896, 1024, 1152, 1280, 1408, 1536, 1792, 2048, 2304, 2688, 3072,
	3200, 3456, 4096, 4864, 5376, 6144, 6528, 6784, 6912, 8192, 9472, 9728, 10240, 10880, 12288, 13568, 14336, 16384,
	18432, 19072, 20480, 21760, 24576, 27264, 28672, 32768}

// alloc is what an allocation of n bytes takes: its size class, or whole 8 KiB pages above 32 KiB.
func alloc(n int64) int64 {
	if n <= 0 {
		return 0
	}
	if n > sizeClasses[len(sizeClasses)-1] {
		return (n + 8191) &^ 8191
	}
	lo, hi := 0, len(sizeClasses)-1
	for lo < hi {
		mid := (lo + hi) / 2
		if sizeClasses[mid] < n {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	return sizeClasses[lo]
}

// stringBytes is what a decoded string's bytes take: below 16 bytes the tiny allocator packs them
// (pointer-free) with others, so they cost about their length.
func stringBytes(n int) int64 {
	if n < 16 {
		return int64(n)
	}
	return alloc(int64(n))
}

// Go's swiss-table maps (Go 1.24+) of string keys and interface values: a header, and up to eight
// entries in one group of eight slots; beyond that a table whose capacity doubles to keep at most
// seven of eight slots full (1,024 slots per table), and a directory of the tables.
const (
	mapHeader    = 48 // maps.Map
	mapGroup     = 8 + 8*(16+16)
	mapTable     = 32 // maps.table
	mapTableSlot = 1024
)

// mapBytes is the memory of a map[string]any with n entries, its keys and values not included.
func mapBytes(n int) int64 {
	switch {
	case n == 0:
		return mapHeader
	case n <= 8:
		return mapHeader + alloc(mapGroup)
	}
	capacity := 16
	for capacity*7/8 < n {
		capacity *= 2
	}
	tables := 1
	if capacity > mapTableSlot {
		tables, capacity = capacity/mapTableSlot, mapTableSlot
	}
	return mapHeader + alloc(int64(8*tables)) + int64(tables)*(alloc(mapTable)+alloc(int64(capacity/8*mapGroup)))
}

// sliceBytes is the backing array of a []any of n elements as append grows it.
func sliceBytes(n int) int64 {
	if n == 0 {
		return 0
	}
	c := 1
	if n > 1 {
		c = 1 << bits.Len(uint(n-1))
	}
	if c > 256 { // append grows by about a quarter beyond 256 elements
		c = n + n/4
	}
	return alloc(int64(c) * 16)
}

// valueBytes is the memory of a value decoded into an interface: what boxing it allocates and
// what it holds.
func valueBytes(v any) int64 {
	switch x := v.(type) {
	case map[string]any:
		return sizeOf(x)
	case []any:
		n := 24 + sliceBytes(len(x)) // the boxed slice header and its array
		for _, e := range x {
			n += valueBytes(e)
		}
		return n
	case string:
		if x == "" {
			return 0
		}
		return 16 + stringBytes(len(x)) // the boxed string header and its bytes
	case int64:
		if x >= 0 && x < 256 { // the runtime's static small integers
			return 0
		}
		return 8
	case float64, int, int32:
		return 8
	}
	return 0 // bool, nil
}

// sizeOf is the memory of a decoded object's map: the map, its keys and its values.
func sizeOf(m map[string]any) int64 {
	n := mapBytes(len(m))
	for k, v := range m {
		n += stringBytes(len(k)) + valueBytes(v)
	}
	return n
}

// objectBytes is the memory one stored object takes: the Unstructured, its content, and the
// store's entry under its key (namespace/name).
func objectBytes(u *unstructured.Unstructured) int64 {
	key := len(u.GetName())
	if ns := u.GetNamespace(); ns != "" {
		key += len(ns) + 1
	}
	return 8 + sizeOf(u.Object) + storeEntryBytes + stringBytes(key)
}

// sizeMemo remembers the size of a scope's objects by UID (else namespace and name) and
// resourceVersion, so that a read walks only what changed. Safe for concurrent use.
type sizeMemo struct {
	mu    sync.Mutex
	gen   uint64
	sizes map[string]memoSize
}

type memoSize struct {
	rv    string
	bytes int64
	gen   uint64 // the read that last saw the object
}

// memoKey identifies an object across its versions.
func memoKey(u *unstructured.Unstructured) string {
	if uid := string(u.GetUID()); uid != "" {
		return uid
	}
	return u.GetNamespace() + "/" + u.GetName()
}

// estimate returns the memory objs take and how many of them it knows the size of (walked now or
// before). It walks at most walkBudget objects; the others count at the known ones' mean. Objects
// that left the scope are forgotten.
func (m *sizeMemo) estimate(objs []*unstructured.Unstructured) (bytes int64, known int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sizes == nil {
		m.sizes = map[string]memoSize{}
	}
	m.gen++
	walked, unknown := 0, 0
	for _, u := range objs {
		if u == nil {
			continue
		}
		k, rv := memoKey(u), u.GetResourceVersion()
		s, ok := m.sizes[k]
		switch {
		case ok && s.rv == rv && rv != "":
		case walked < walkBudget:
			s = memoSize{rv: rv, bytes: objectBytes(u)}
			walked++
		default:
			unknown++
			continue
		}
		s.gen = m.gen
		m.sizes[k] = s
		bytes += s.bytes
		known++
	}
	for k, s := range m.sizes {
		if s.gen != m.gen {
			delete(m.sizes, k)
		}
	}
	if unknown > 0 && known > 0 {
		bytes += bytes / int64(known) * int64(unknown)
	}
	return bytes, known
}

// storeObjects returns a store's objects (the informer's: *unstructured.Unstructured).
func storeObjects(list []any) []*unstructured.Unstructured {
	out := make([]*unstructured.Unstructured, 0, len(list))
	for _, o := range list {
		if u, ok := o.(*unstructured.Unstructured); ok {
			out = append(out, u)
		}
	}
	return out
}
