package model

import (
	"reflect"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
)

// TestObjectYAMLOrder (WF15, D-86): apiVersion, kind, metadata (name, namespace, labels,
// annotations, then the rest), the body sorted, status last.
func TestObjectYAMLOrder(t *testing.T) {
	obj := map[string]any{
		"status": map[string]any{"phase": "Active"},
		"data":   map[string]any{"b": "2", "a": "1"},
		"kind":   "ConfigMap",
		"metadata": map[string]any{
			"uid": "u-1", "resourceVersion": "805827", "creationTimestamp": "2026-10-17T10:00:00Z",
			"namespace": "walk-test", "name": "walk-cm", "labels": map[string]any{"app": "walk"},
			"annotations": map[string]any{"note": "x"},
		},
		"apiVersion": "v1",
		"binaryData": map[string]any{"moved": "AAE="},
	}
	got, err := ObjectYAML(obj)
	if err != nil {
		t.Fatal(err)
	}
	want := `apiVersion: v1
kind: ConfigMap
metadata:
  name: walk-cm
  namespace: walk-test
  labels:
    app: walk
  annotations:
    note: x
  creationTimestamp: "2026-10-17T10:00:00Z"
  resourceVersion: "805827"
  uid: u-1
binaryData:
  moved: AAE=
data:
  a: "1"
  b: "2"
status:
  phase: Active
`
	if string(got) != want {
		t.Errorf("ObjectYAML:\n%s\nwant:\n%s", got, want)
	}
}

// TestObjectYAMLRoundTrip: the ordered text parses to the same object, its scalars quoted as
// yaml.Marshal quotes them (strings that read as booleans, numbers or null stay strings), block
// scalars with empty lines and leading blanks in metadata kept, lists under metadata valid.
func TestObjectYAMLRoundTrip(t *testing.T) {
	tricky := []any{"on", "yes", "no", "off", "y", "null", "~", "0x1f", "1e3", "012", "true", "", " lead", "trail ",
		"a: b", "- x", "#c", "multi\nline\n\nwith empty\n", "  indented\n\n  block\n", "ünïcode ✓", strings.Repeat("long ", 40)}
	meta := map[string]any{"name": "x", "namespace": "n", "finalizers": []any{"a/b", "c"},
		"annotations": map[string]any{}, "labels": map[string]any{"k": "on"}}
	data := map[string]any{}
	for i, v := range tricky {
		data[string(rune('a'+i))] = v
		meta["annotations"].(map[string]any)["k"+string(rune('a'+i))] = v
	}
	obj := map[string]any{
		"apiVersion": "apps/v1", "kind": "Deployment", "metadata": meta, "data": data,
		"spec":   map[string]any{"replicas": int64(3), "list": []any{map[string]any{"b": nil, "a": []any{}}, "s"}, "empty": map[string]any{}},
		"status": map[string]any{"conditions": []any{map[string]any{"type": "Ready", "status": "True"}}},
	}
	for _, o := range []map[string]any{obj, {}, {"kind": "List"}, {"metadata": map[string]any{}}} {
		y, err := ObjectYAML(o)
		if err != nil {
			t.Fatal(err)
		}
		var back map[string]any
		if err := yaml.Unmarshal(y, &back); err != nil {
			t.Fatalf("the ordered YAML does not parse: %v\n%s", err, y)
		}
		plain, _ := yaml.Marshal(o)
		var want map[string]any
		_ = yaml.Unmarshal(plain, &want)
		if !reflect.DeepEqual(back, want) {
			t.Errorf("round trip differs:\n%s", y)
		}
	}
}
