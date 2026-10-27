package kinds_test

import (
	"encoding/json"
	"fmt"
	"reflect"
	"runtime/debug"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	utiljson "k8s.io/apimachinery/pkg/util/json"

	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
)

// exercise runs every function of a profile on an object and reports a panic or a row of the
// wrong width as an error. It calls the profile's functions directly: model.BuildRow and
// model.FieldValues recover from panics, which would hide one here.
func exercise(p *model.KindProfile, obj *unstructured.Unstructured) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%s: panic: %v\n%s", p.Key, r, debug.Stack())
		}
	}()
	cells, _ := cellsOf(p, obj)
	if len(cells) != len(p.Columns) {
		return fmt.Errorf("%s: %d cells for %d columns", p.Key, len(cells), len(p.Columns))
	}
	for i, c := range cells {
		_ = model.CellText(p.Columns[i], c, now)
	}
	row := model.BuildRow(p, "fuzz", obj)
	_ = model.MatchesFilter(row, "a b")
	for _, f := range p.Fields {
		_ = f(obj)
	}
	for _, name := range []string{"name", "namespace", "label"} { // the profile's own fields ran above
		_ = model.FieldValues(p, name, obj)
	}
	_ = model.MergeProperties(model.PropertiesSection(obj, now), detailOf(p, obj))
	return nil
}

// fuzzProfile feeds arbitrary JSON to a profile, decoded both the way the cache decodes it
// (integers as int64) and the way plain encoding/json does (every number a float64).
func fuzzProfile(f *testing.F, key string) {
	p, ok := kinds.ByKey(key)
	if !ok {
		f.Fatalf("no profile %s", key)
	}
	for _, c := range loadCases(f, key) {
		f.Add(c.json)
	}
	for _, seed := range []string{`{}`, `{"metadata":null,"spec":null,"status":null}`,
		`{"metadata":{"name":1,"labels":[]},"spec":[],"status":"x"}`} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		decoders := []func([]byte, any) error{utiljson.Unmarshal, json.Unmarshal}
		for _, decode := range decoders {
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

func FuzzNodes(f *testing.F)       { fuzzProfile(f, "nodes") }
func FuzzPods(f *testing.F)        { fuzzProfile(f, "pods") }
func FuzzDeployments(f *testing.F) { fuzzProfile(f, "deployments") }
func FuzzServices(f *testing.F)    { fuzzProfile(f, "services") }
func FuzzNamespaces(f *testing.F)  { fuzzProfile(f, "namespaces") }
func FuzzEvents(f *testing.F)      { fuzzProfile(f, "events") }

// hostile are the values TestTypeConfusion puts in place of each field: null, every JSON type,
// Go integer types JSON never yields, and numbers that are not integers.
var hostile = []any{
	nil, "", "x", int64(-1), int(7), uint64(1 << 63), 1.5, float64(1e300), true,
	[]any{}, []any{nil, "x", int64(1)}, map[string]any{}, map[string]any{"": nil},
}

// TestTypeConfusion replaces every field of every recorded object, one at a time, with each
// hostile value and runs the whole profile. Byte-level fuzzing rarely produces well-formed
// objects with one wrong type deep inside; this does it exhaustively.
func TestTypeConfusion(t *testing.T) {
	if raceEnabled {
		t.Skip("one goroutine: the build without the race detector runs it (make test-norace-coverage)")
	}
	variants := 0
	defer func() { t.Logf("%d object variants exercised", variants) }()
	for _, p := range kinds.Builtin() {
		for _, c := range loadCases(t, p.Key) {
			variants += confuse(t, p.Key+"/"+c.name, c.obj, func(path []any, v any) {
				if err := exercise(p, c.obj); err != nil {
					t.Errorf("%s/%s with %v = %#v: %v", p.Key, c.name, path, v, err)
				}
			})
		}
	}
}

// confuse puts each hostile value in place of each value of obj in turn, calls try with obj so
// changed, and puts the value back; it returns the number of variants. A profile that changed the
// object it was given otherwise fails the test: the cache's objects are shared.
func confuse(t *testing.T, what string, obj *unstructured.Unstructured, try func(path []any, v any)) int {
	t.Helper()
	pristine := obj.DeepCopy() // before any hostile value: DeepCopy rejects Go-only types
	var paths [][]any
	collectPaths(obj.Object, nil, &paths)
	for _, path := range paths {
		for _, v := range hostile {
			was := setPath(obj.Object, path, v)
			try(path, v)
			setPath(obj.Object, path, was)
		}
	}
	if !reflect.DeepEqual(obj.Object, pristine.Object) {
		t.Errorf("%s: a profile changed the object it was given", what)
	}
	return len(paths) * len(hostile)
}

// collectPaths lists the path to every value in a JSON tree: map keys as strings, list indices
// as ints.
func collectPaths(v any, prefix []any, out *[][]any) {
	switch t := v.(type) {
	case map[string]any:
		for k, e := range t {
			p := append(append([]any(nil), prefix...), k)
			*out = append(*out, p)
			collectPaths(e, p, out)
		}
	case []any:
		for i, e := range t {
			p := append(append([]any(nil), prefix...), i)
			*out = append(*out, p)
			collectPaths(e, p, out)
		}
	}
}

// setPath puts v at path in root and returns the value it replaced.
func setPath(root any, path []any, v any) (was any) {
	cur := root
	for i, step := range path {
		last := i == len(path)-1
		switch s := step.(type) {
		case string:
			m := cur.(map[string]any)
			if last {
				was, m[s] = m[s], v
				return was
			}
			cur = m[s]
		case int:
			l := cur.([]any)
			if last {
				was, l[s] = l[s], v
				return was
			}
			cur = l[s]
		}
	}
	return nil
}
