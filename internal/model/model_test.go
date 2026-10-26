package model

import (
	"flag"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// The kinds goldens regenerate with `go test ./internal/model/... -update`, which hands the flag
// to this package too; model has no golden files, so it only accepts the flag.
var _ = flag.Bool("update", false, "rewrite golden files (used by package kinds)")

var now = time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)

func TestFormatAge(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{-time.Hour, "0s"},
		{0, "0s"},
		{59 * time.Second, "59s"},
		{time.Minute, "1m"},
		{59*time.Minute + 59*time.Second, "59m"},
		{time.Hour, "1h"},
		{23 * time.Hour, "23h"},
		{24 * time.Hour, "1d"},
		{364 * 24 * time.Hour, "364d"},
		{365 * 24 * time.Hour, "1y"},
		{3 * 365 * 24 * time.Hour, "3y"},
	}
	for _, tt := range tests {
		if got := FormatAge(tt.d); got != tt.want {
			t.Errorf("FormatAge(%v) = %q, want %q", tt.d, got, tt.want)
		}
	}
}

func TestCellText(t *testing.T) {
	age := Column{ID: "age", Type: ColumnAge}
	num := Column{ID: "restarts", Type: ColumnNumber}
	text := Column{ID: "name", Type: ColumnText}
	tests := []struct {
		col  Column
		cell Cell
		want string
	}{
		{age, Cell{Num: now.Add(-90 * time.Minute).Unix()}, "1h"},
		{age, Cell{Text: "-"}, "-"},
		{age, Cell{}, ""},
		{num, Cell{Num: 42}, "42"},
		{num, Cell{Num: 0}, "0"},
		{num, Cell{Text: "3/4", Num: 3}, "3/4"},
		{text, Cell{Text: "web", Num: 9}, "web"},
	}
	for _, tt := range tests {
		if got := CellText(tt.col, tt.cell, now); got != tt.want {
			t.Errorf("CellText(%s, %+v) = %q, want %q", tt.col.Type, tt.cell, got, tt.want)
		}
	}
}

func TestMatchesFilter(t *testing.T) {
	row := Row{Cells: []Cell{{Text: "web-0"}, {Text: "Shop"}, {Text: "CrashLoopBackOff"}}}
	tests := []struct {
		filter string
		want   bool
	}{
		{"", true},
		{"   ", true},
		{"web", true},
		{"SHOP crash", true},
		{"web missing", false},
		{"loopback", true},
		{"shop-web", false},
	}
	for _, tt := range tests {
		if got := MatchesFilter(row, tt.filter); got != tt.want {
			t.Errorf("MatchesFilter(%q) = %v, want %v", tt.filter, got, tt.want)
		}
	}
}

func TestObjectRef(t *testing.T) {
	pod := ObjectRef{Context: "kind-a", Kind: "pods", Namespace: "shop", Name: "web-0"}
	node := ObjectRef{Kind: "nodes", Name: "worker-1"}
	if pod.Key() != "shop/web-0" || node.Key() != "worker-1" {
		t.Errorf("Key = %q, %q", pod.Key(), node.Key())
	}
	if pod.String() != "pods/shop/web-0" || node.String() != "nodes/worker-1" {
		t.Errorf("String = %q, %q", pod.String(), node.String())
	}
	anyContext := pod
	anyContext.Context = ""
	other := pod
	other.Context = "kind-b"
	renamed := pod
	renamed.Name = "web-1"
	switch {
	case !pod.Same(pod), !pod.Same(anyContext), !anyContext.Same(pod):
		t.Error("Same should match itself and an empty context")
	case pod.Same(other), pod.Same(renamed), pod.Same(node):
		t.Error("Same matched a different object")
	}
}

func TestRedact(t *testing.T) {
	secret := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1",
		"kind":       "Secret",
		"metadata": map[string]any{"name": "db", "annotations": map[string]any{
			"kubectl.kubernetes.io/last-applied-configuration": `{"data":{"password":"aHVudGVyMg=="}}`,
			"owner": "team-a",
		}},
		"data":       map[string]any{"password": "aHVudGVyMg=="},
		"stringData": map[string]any{"user": "admin"},
		"type":       "Opaque",
	}}
	out := Redact(secret)
	if out == secret {
		t.Fatal("Redact must copy a Secret")
	}
	if NestedString(out, "data", "password") != Redacted || NestedString(out, "stringData", "user") != Redacted {
		t.Errorf("values not redacted: %v", out.Object)
	}
	ann := out.GetAnnotations()
	if ann["kubectl.kubernetes.io/last-applied-configuration"] != Redacted || ann["owner"] != "team-a" {
		t.Errorf("annotations = %v", ann)
	}
	if NestedString(secret, "data", "password") != "aHVudGVyMg==" {
		t.Error("Redact modified its input")
	}
	cm := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "ConfigMap"}}
	if Redact(cm) != cm || Redact(nil) != nil {
		t.Error("non-Secrets are returned unchanged")
	}
	odd := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "Secret", "data": "garbage"}}
	if Redact(odd) == nil {
		t.Error("a Secret with malformed data should still be returned")
	}
}

// A Helm release record kept in a ConfigMap (HELM_DRIVER=configmap) loses its payload, which holds
// the release's values, as a Secret loses its values; its other keys and every other ConfigMap stay.
func TestRedactHelmRecord(t *testing.T) {
	record := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1", "kind": "ConfigMap",
		"metadata": map[string]any{"name": "sh.helm.release.v1.web.v1", "labels": map[string]any{"owner": "helm", "name": "web"},
			"annotations": map[string]any{"kubectl.kubernetes.io/last-applied-configuration": "{}"}},
		"data": map[string]any{"release": "H4sIAAAAAAAC/6pWSkosSlWyUkpKzVGqBQQAAP//", "note": "kept"},
	}}
	if !IsHelmRecord(record) {
		t.Fatal("not a Helm record")
	}
	out := Redact(record)
	if out == record || NestedString(out, "data", "release") != Redacted || NestedString(out, "data", "note") != "kept" ||
		out.GetAnnotations()["kubectl.kubernetes.io/last-applied-configuration"] != Redacted {
		t.Errorf("redacted %v", out.Object)
	}
	if NestedString(record, "data", "release") == Redacted {
		t.Error("Redact modified its input")
	}
	plain := record.DeepCopy()
	plain.SetLabels(map[string]string{"owner": "team-a"})
	if IsHelmRecord(plain) || Redact(plain) != plain {
		t.Error("a ConfigMap Helm did not write is redacted")
	}
	bare := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "ConfigMap",
		"metadata": map[string]any{"labels": map[string]any{"owner": "helm"}}, "data": "not a map"}}
	if Redact(bare) == nil {
		t.Error("a record with malformed data should still be returned")
	}
}

func TestPropertiesSection(t *testing.T) {
	obj := &unstructured.Unstructured{Object: map[string]any{
		"metadata": map[string]any{
			"name":              "web-0",
			"namespace":         "shop",
			"uid":               "uid-1",
			"creationTimestamp": "2026-10-01T12:00:00Z",
			"labels":            map[string]any{"b": "2", "a": "1"},
			"annotations": map[string]any{
				"kubectl.kubernetes.io/last-applied-configuration": "{}",
				"note": "x",
			},
			"ownerReferences": []any{
				map[string]any{"apiVersion": "apps/v1", "kind": "ReplicaSet", "name": "web-1a2b"},
				map[string]any{"apiVersion": "example.com/v1", "kind": "Widget", "name": "w"},
			},
		},
	}}
	s := PropertiesSection(obj, now)
	if s.Title != TitleProperties {
		t.Fatalf("title %q", s.Title)
	}
	var order []string
	byName := map[string]DetailItem{}
	for _, it := range s.Items {
		order = append(order, it.Name)
		if _, dup := byName[it.Name]; !dup {
			byName[it.Name] = it
		}
	}
	// Lens shows no UID; Controlled By once per owner (capture, 2026-10-11).
	want := []string{"Created", "Name", "Namespace", "Labels", "Annotations", "Controlled By", "Controlled By"}
	if strings.Join(order, ",") != strings.Join(want, ",") {
		t.Fatalf("order = %v, want %v", order, want)
	}
	// The time is a mark that the engine writes in the user's zone and format (D-31).
	if it := byName["Created"]; it.Value != "10d ago ("+MarkTime(time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC))+")" ||
		(TimeLayout{}).In(time.UTC).WriteMarks(it.Value) != "10d ago (2026-10-01 12:00:00)" {
		t.Errorf("created = %q", it.Value)
	}
	if it := byName["Namespace"]; it.Value != "shop" || it.Link == nil || *it.Link != (ObjectRef{Kind: "namespaces", Name: "shop"}) {
		t.Errorf("namespace = %+v", it)
	}
	if it := byName["Labels"]; it.Value != "a=1\nb=2" || it.Fold != "Labels" || len(it.Chips) != 2 || it.Chips[0].Text != "a=1" || it.Entries() != 2 {
		t.Errorf("labels = %+v", it)
	}
	if it := byName["Annotations"]; it.Value != "note=x" || it.Fold != "Annotations" || it.Entries() != 1 {
		t.Errorf("annotations = %+v", it)
	}
	owner := s.Items[5]
	if owner.Value != "ReplicaSet web-1a2b" || owner.Link == nil || *owner.Link != (ObjectRef{Kind: "replicasets", Namespace: "shop", Name: "web-1a2b"}) {
		t.Errorf("controlled by = %+v", owner)
	}
	if custom := s.Items[6]; custom.Value != "Widget w" || custom.Link != nil {
		t.Errorf("a custom owner kind must stay unlinked: %+v", custom)
	}
	// With a link for custom kinds (the engine's drawer: the definitions), the custom owner links;
	// the built-in one still does.
	widgets := func(apiVersion, kind, namespace, name string) *ObjectRef {
		if kind == "Widget" {
			return &ObjectRef{Kind: "widgets.example.com", Namespace: namespace, Name: name}
		}
		return RefTo(apiVersion, kind, namespace, name)
	}
	linked := PropertiesSectionLinked(obj, now, widgets)
	if l := linked.Items[6].Link; l == nil || *l != (ObjectRef{Kind: "widgets.example.com", Namespace: "shop", Name: "w"}) {
		t.Errorf("the custom owner with a link for custom kinds: %+v", linked.Items[6])
	}
	if l := linked.Items[5].Link; l == nil || l.Kind != "replicasets" {
		t.Errorf("the built-in owner with a link for custom kinds: %+v", linked.Items[5])
	}

	bare := PropertiesSection(&unstructured.Unstructured{Object: map[string]any{"metadata": map[string]any{"name": "n"}}}, now)
	if len(bare.Items) != 1 || bare.Items[0].Name != "Name" {
		t.Errorf("bare properties = %+v", bare.Items)
	}
}

func TestMergeProperties(t *testing.T) {
	props := DetailSection{Title: TitleProperties, Items: []DetailItem{{Name: "Name", Value: "n"}}}
	kind := []DetailSection{
		{Title: TitleProperties, Items: []DetailItem{{Name: "Status", Value: "Running"}}},
		{Title: "Pod Volumes"},
	}
	got := MergeProperties(props, kind)
	if len(got) != 2 || got[0].Title != TitleProperties || len(got[0].Items) != 2 || got[0].Items[1].Name != "Status" || got[1].Title != "Pod Volumes" {
		t.Errorf("merged = %+v", got)
	}
	got = MergeProperties(props, []DetailSection{{Title: "Status"}})
	if len(got) != 2 || len(got[0].Items) != 1 || got[1].Title != "Status" {
		t.Errorf("no kind properties = %+v", got)
	}
}

// TestCellTable checks a drawer table of cells: Rows holds the texts agents read, Types and Cells
// what the drawer draws, and a short row is padded.
func TestCellTable(t *testing.T) {
	pod := &ObjectRef{Kind: "pods", Namespace: "shop", Name: "web-0"}
	tbl := CellTable([]Column{
		{ID: "name", Title: "Name", Type: ColumnText},
		{ID: "warning", Title: "Warning", Type: ColumnWarning},
		{ID: "restarts", Title: "Restarts", Type: ColumnNumber},
	}, [][]Cell{
		{{Text: "web-0", Link: pod}, {Text: "CrashLoopBackOff", Num: 1, Status: StatusWarning}, {Num: 4}},
		{{Text: "web-1"}},
	})
	if !slices.Equal(tbl.Columns, []string{"Name", "Warning", "Restarts"}) ||
		!slices.Equal(tbl.Types, []ColumnType{ColumnText, ColumnWarning, ColumnNumber}) {
		t.Errorf("columns %q, types %q", tbl.Columns, tbl.Types)
	}
	if len(tbl.Rows) != 2 || !slices.Equal(tbl.Rows[0], []string{"web-0", "CrashLoopBackOff", "4"}) ||
		!slices.Equal(tbl.Rows[1], []string{"web-1", "", "0"}) {
		t.Errorf("rows %q", tbl.Rows)
	}
	if len(tbl.Cells) != 2 || len(tbl.Cells[1]) != 3 || tbl.Cells[0][0].Link != pod || tbl.Cells[0][1].Status != StatusWarning {
		t.Errorf("cells %+v", tbl.Cells)
	}
}

// A table's ages are written against the engine's clock: a section with an Age column is copied
// with its texts written, the others shared, the input kept.
func TestWriteAges(t *testing.T) {
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	ages := CellTable([]Column{
		{ID: "name", Title: "Name", Type: ColumnText},
		{ID: "age", Title: "Age", Type: ColumnAge},
	}, [][]Cell{
		{{Text: "web-0"}, AgeCell(now.Add(-2 * time.Hour))},
		{{Text: "web-1"}, {}},
	})
	plain := CellTable([]Column{{ID: "name", Title: "Name", Type: ColumnText}}, [][]Cell{{{Text: "web-0"}}})
	in := []DetailSection{{Title: "Pods", Table: ages}, {Title: "Other", Table: plain}, {Title: "Items"}}
	out := WriteAges(in, now)
	if got := out[0].Table.Rows; !slices.Equal(got[0], []string{"web-0", "2h"}) || !slices.Equal(got[1], []string{"web-1", ""}) {
		t.Errorf("rows %q, want the age against now and an unknown age empty", got)
	}
	if ages.Rows[0][1] == "2h" || out[0].Table == ages {
		t.Error("the input's table was written")
	}
	if out[1].Table != plain {
		t.Error("a table without ages was copied")
	}
	if none := []DetailSection{{Title: "Other", Table: plain}}; &WriteAges(none, now)[0] != &none[0] {
		t.Error("sections without ages were copied")
	}
}

func TestPartsCell(t *testing.T) {
	c := PartsCell(StatusOK, CellPart{Text: "SchedulingDisabled"}, CellPart{}, CellPart{Text: "Ready", Status: StatusOK})
	if c.Text != "SchedulingDisabled Ready" || c.Status != StatusOK || c.Parts == nil || len(*c.Parts) != 2 {
		t.Fatalf("cell = %+v", c)
	}
	if one := PartsCell(StatusError, CellPart{Text: "NotReady", Status: StatusError}); one.Parts != nil || one.Text != "NotReady" || one.Status != StatusError {
		t.Errorf("one part = %+v", one)
	}
	if none := PartsCell(StatusUnknown); none != (Cell{Status: StatusUnknown}) {
		t.Errorf("no parts = %+v", none)
	}
}

func TestBuiltinKindKey(t *testing.T) {
	for _, tt := range []struct {
		apiVersion, kind, key string
		namespaced, ok        bool
	}{
		{"apps/v1", "ReplicaSet", "replicasets", true, true},
		{"batch/v1", "Job", "jobs", true, true},
		{"v1", "Node", "nodes", false, true},
		{"", "DaemonSet", "daemonsets", true, true},
		{"apps/v1", "Node", "", false, false},
		{"cert-manager.io/v1", "Certificate", "", false, false},
	} {
		key, namespaced, ok := BuiltinKindKey(tt.apiVersion, tt.kind)
		if key != tt.key || namespaced != tt.namespaced || ok != tt.ok {
			t.Errorf("BuiltinKindKey(%q, %q) = %q %v %v", tt.apiVersion, tt.kind, key, namespaced, ok)
		}
	}
	if r := RefTo("v1", "Node", "ignored", "worker-1"); r == nil || *r != (ObjectRef{Kind: "nodes", Name: "worker-1"}) {
		t.Errorf("RefTo node = %+v", r)
	}
	if r := RefTo("apps/v1", "ReplicaSet", "shop", ""); r != nil {
		t.Errorf("RefTo without a name = %+v", r)
	}
}

func testProfile() *KindProfile {
	return &KindProfile{
		Key: "widgets", Group: "example.com", Version: "v1", Resource: "widgets", Kind: "Widget",
		Columns: []Column{{ID: "name"}, {ID: "size", Type: ColumnNumber}, {ID: "age", Type: ColumnAge}},
		Cells: func(obj *unstructured.Unstructured) ([]Cell, Status) {
			n, _ := NestedInt64(obj, "spec", "size")
			return []Cell{{Text: obj.GetName()}, {Num: n}}, StatusOK // one cell short on purpose
		},
		Fields: map[string]func(*unstructured.Unstructured) []string{
			"color": func(obj *unstructured.Unstructured) []string {
				return []string{NestedString(obj, "spec", "color")}
			},
			"boom": func(*unstructured.Unstructured) []string { panic("boom") },
		},
	}
}

func widget(name string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"metadata": map[string]any{
			"name": name, "namespace": "ns", "uid": "u1", "resourceVersion": "7",
			"labels": map[string]any{"a": "1", "b": "2"},
		},
		"spec": map[string]any{"size": int64(5), "color": "blue"},
	}}
}

func TestProfileBasics(t *testing.T) {
	p := testProfile()
	if gvr := p.GVR(); gvr.Group != "example.com" || gvr.Version != "v1" || gvr.Resource != "widgets" {
		t.Errorf("GVR = %v", gvr)
	}
	if p.ColumnIndex("size") != 1 || p.ColumnIndex("missing") != -1 {
		t.Error("ColumnIndex")
	}
	names := p.FieldNames()
	if !slices.Equal(names[:3], []string{"name", "namespace", "label"}) || len(names) != 5 {
		t.Errorf("FieldNames = %v", names)
	}
}

func TestBuildRow(t *testing.T) {
	p := testProfile()
	row := BuildRow(p, "ctx", widget("w1"))
	wantRef := ObjectRef{Context: "ctx", Kind: "widgets", Namespace: "ns", Name: "w1"}
	if row.Ref != wantRef || row.UID != "u1" || row.ResourceVersion != "7" || row.Status != StatusOK {
		t.Errorf("row = %+v", row)
	}
	if len(row.Cells) != 3 || row.Cells[1].Num != 5 || row.Cells[2] != (Cell{}) {
		t.Errorf("short Cells not padded: %+v", row.Cells)
	}

	deleted := widget("w2")
	_ = unstructured.SetNestedField(deleted.Object, "2026-10-11T11:00:00Z", "metadata", "deletionTimestamp")
	if BuildRow(p, "ctx", deleted).Status != StatusTerminating {
		t.Error("a deletion timestamp should make the row terminating")
	}

	p.Cells = func(*unstructured.Unstructured) ([]Cell, Status) { panic("bad profile") }
	row = BuildRow(p, "ctx", widget("w3"))
	if row.Status != StatusUnknown || len(row.Cells) != 3 || row.Cells[0].Text != "w3" {
		t.Errorf("panicking Cells = %+v", row)
	}

	p.Cells = nil
	row = BuildRow(p, "ctx", widget("w4"))
	if len(row.Cells) != 3 || row.Cells[0].Text != "w4" || row.Status != StatusNone {
		t.Errorf("nil Cells = %+v", row)
	}

	p.Columns = nil
	p.Cells = func(*unstructured.Unstructured) ([]Cell, Status) { panic("no columns") }
	if row := BuildRow(p, "ctx", widget("w5")); len(row.Cells) != 0 {
		t.Errorf("no columns = %+v", row.Cells)
	}
}

func TestFieldValues(t *testing.T) {
	p := testProfile()
	obj := widget("w1")
	labels := FieldValues(p, "label", obj)
	sort.Strings(labels)
	if !slices.Equal(labels, []string{"a=1", "b=2"}) {
		t.Errorf("label = %v", labels)
	}
	if got := FieldValues(p, "labels", obj); len(got) != 2 {
		t.Errorf("labels alias = %v", got)
	}
	if got := FieldValues(p, "name", obj); !slices.Equal(got, []string{"w1"}) {
		t.Errorf("name = %v", got)
	}
	if got := FieldValues(p, "namespace", obj); !slices.Equal(got, []string{"ns"}) {
		t.Errorf("namespace = %v", got)
	}
	if got := FieldValues(p, "color", obj); !slices.Equal(got, []string{"blue"}) {
		t.Errorf("color = %v", got)
	}
	if got := FieldValues(p, "boom", obj); got != nil {
		t.Errorf("a panicking field should yield nil, got %v", got)
	}
	if got := FieldValues(p, "unknown", obj); got != nil {
		t.Errorf("unknown field = %v", got)
	}
}
