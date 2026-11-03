package engine

import (
	"slices"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// TestDefinitionsGroupFilterAndColumns: the Definitions view lists the definitions of the groups in
// UIState.Groups only, other views ignore the group filter, and a view's snapshot hides the columns
// UIState.HiddenColumns lists for it, or its kind's defaults without an entry.
func TestDefinitionsGroupFilterAndColumns(t *testing.T) {
	defs, ok := kinds.ByKey(kinds.KeyCustomResourceDefinitions)
	if !ok {
		t.Fatal("no Definitions profile")
	}
	src := testkit.NewMemorySource()
	src.Set(crdsGVR, orderCRD())
	src.Set(crdsGVR, testkit.CRD("example.com", "widgets", "Widget", true,
		testkit.CRDVersion{Name: "v1", Served: true, Storage: true}))
	src.Set(testkit.PodsGVR, testkit.Pod("default", "web-0"))
	s := newCRDSession(t, src, defs)
	openView(s, kinds.KeyCustomResourceDefinitions)
	snap := waitTable(t, s, kinds.KeyCustomResourceDefinitions, func(t *TableSnapshot) bool { return len(t.Rows) == 2 })
	if got := hiddenOf(snap); !slices.Equal(got, []string{"name"}) {
		t.Fatalf("default hidden columns %v, want the Definitions view's hidden name", got)
	}

	s.UpdateUI(func(u *UIState) { u.Groups = []string{orderGroup} })
	snap = waitTable(t, s, kinds.KeyCustomResourceDefinitions, func(t *TableSnapshot) bool { return len(t.Rows) == 1 })
	if snap.Rows[0].Ref.Name != "orders."+orderGroup || snap.Total != 2 {
		t.Fatalf("filtered by group: %s, total %d", summary(snap), snap.Total)
	}

	s.UpdateUI(func(u *UIState) {
		u.HiddenColumns = map[string][]string{kinds.KeyCustomResourceDefinitions: {"scope", "age"}}
	})
	snap = waitTable(t, s, kinds.KeyCustomResourceDefinitions, func(t *TableSnapshot) bool {
		return slices.Equal(hiddenOf(t), []string{"scope", "age"})
	})
	if len(snap.Rows) != 1 || len(snap.Rows[0].Cells) != len(snap.Columns) {
		t.Fatalf("rows no longer fit the columns: %s", summary(snap))
	}

	// The group filter is the Definitions view's only.
	openView(s, "pods")
	waitTable(t, s, "pods", func(t *TableSnapshot) bool { return len(t.Rows) == 1 })
}

// TestWriteCSV: the header holds the visible columns' titles, every row the cells as the table
// shows them (ages relative to now), quoted where CSV needs it; cells a spreadsheet would run as a
// formula are neutralised, plain numbers are not.
func TestWriteCSV(t *testing.T) {
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	snap := &TableSnapshot{
		View: "events",
		Columns: []model.Column{
			{ID: "name", Title: "Name", Type: model.ColumnText, Hidden: true},
			{ID: "message", Title: "Message", Type: model.ColumnText},
			{ID: "count", Title: "Count", Type: model.ColumnNumber},
			{ID: "signals", Title: "Signals", Type: model.ColumnSignals},
			{ID: "age", Title: "Age", Type: model.ColumnAge},
		},
		Rows: []model.Row{
			{Cells: []model.Cell{{Text: "a"}, {Text: `Back-off "restarting", failed`}, {Num: 3}, {}, {Num: now.Add(-5 * time.Minute).Unix()}}},
			{Cells: []model.Cell{{Text: "b"}, {Text: "=HYPERLINK(\"http://x\")"}, {Num: -1}, model.SignalsCell([]model.Signal{{Source: model.SignalState, Text: "3 restarts", Status: model.StatusWarning}}), {}}},
			{Cells: []model.Cell{{Text: "c"}, {Text: "line one\nline two"}, {Text: "-"}, {}, {Num: now.Add(-49 * time.Hour).Unix()}}},
			{Cells: []model.Cell{{Text: "d"}, {Text: "@SUM(A1)"}, {Text: "+1"}, {}, {}}},
			{Cells: []model.Cell{{Text: "e"}, {Text: "-cmd|' /C calc'!A0"}, {}, {}, {}}},
		},
	}
	var b strings.Builder
	if err := WriteCSV(&b, snap, now); err != nil {
		t.Fatal(err)
	}
	want := "Message,Count,Signals,Age\n" +
		"\"Back-off \"\"restarting\"\", failed\",3,,5m\n" +
		"\"'=HYPERLINK(\"\"http://x\"\")\",-1,3 restarts,\n" +
		"\"line one\nline two\",-,,2d\n" +
		"'@SUM(A1),+1,,\n" +
		"'-cmd|' /C calc'!A0,0,,\n"
	if got := b.String(); got != want {
		t.Fatalf("CSV:\n%s\nwant:\n%s", got, want)
	}
}

// TestCSVOfMetadataOnlyKind: a kind the cache holds as metadata only (Secrets, H-8) exports no
// values: the CSV is built from the snapshot's cells, which see only metadata.
func TestCSVOfMetadataOnlyKind(t *testing.T) {
	// Its own key: the fixtures' "secrets" profile watches full objects.
	secrets := &model.KindProfile{
		Key: "metasecrets", Version: "v1", Resource: "secrets", Kind: "Secret", Title: "Secrets", Namespaced: true,
		MetadataOnly: true,
		Columns: []model.Column{
			{ID: "name", Title: "Name", Type: model.ColumnText},
			{ID: "data", Title: "Data", Type: model.ColumnText}, // what a careless profile might try
		},
		Cells: func(obj *unstructured.Unstructured) ([]model.Cell, model.Status) {
			v, _, _ := unstructured.NestedString(obj.Object, "data", "password")
			return []model.Cell{{Text: obj.GetName()}, {Text: v}}, model.StatusNone
		},
	}
	src := testkit.NewMemorySource()
	src.Set(secretsGVR, testkit.Object("v1", "Secret", "shop", "db", map[string]any{
		"data": map[string]any{"password": "aHVudGVyMg=="}, "stringData": map[string]any{"token": "hunter2"},
	}))
	s := newCRDSession(t, src, secrets)
	openView(s, "metasecrets")
	snap := waitTable(t, s, "metasecrets", func(t *TableSnapshot) bool { return len(t.Rows) == 1 })
	var b strings.Builder
	if err := WriteCSV(&b, snap, time.Now()); err != nil {
		t.Fatal(err)
	}
	if got := b.String(); got != "Name,Data\ndb,\n" || strings.Contains(got, "aHVudGVyMg") || strings.Contains(got, "hunter2") {
		t.Fatalf("CSV of a metadata-only kind: %q", got)
	}
}

func hiddenOf(t *TableSnapshot) []string {
	var out []string
	for _, c := range t.Columns {
		if c.Hidden {
			out = append(out, c.ID)
		}
	}
	return out
}

// TestColumnOrder: the ids first, in their order (unknown and repeated ones skipped), then the
// other columns in the kind's order; nil for none and for the kind's own order; the CSV follows it.
func TestColumnOrder(t *testing.T) {
	cols := []model.Column{{ID: "name", Title: "Name"}, {ID: "namespace", Title: "Namespace"}, {ID: "status", Title: "Status"}, {ID: "age", Title: "Age"}}
	for _, tc := range []struct {
		ids  []string
		want []int
	}{
		{nil, nil},
		{[]string{"age", "name"}, []int{3, 0, 1, 2}},
		{[]string{"status", "x", "status"}, []int{2, 0, 1, 3}},
		{[]string{"name", "namespace"}, nil},
		{[]string{"name", "namespace", "status", "age"}, nil},
	} {
		if got := ColumnOrder(cols, tc.ids); !slices.Equal(got, tc.want) {
			t.Errorf("ColumnOrder(%v) = %v, want %v", tc.ids, got, tc.want)
		}
	}
	snap := &TableSnapshot{Columns: cols}
	if got := snap.DisplayOrder(); !slices.Equal(got, []int{0, 1, 2, 3}) {
		t.Errorf("DisplayOrder without an order: %v", got)
	}
	snap.Order = []int{3, 0, 1, 2}
	snap.Columns[1].Hidden = true
	snap.Rows = []model.Row{{Cells: []model.Cell{{Text: "web"}, {Text: "shop"}, {Text: "ok"}, {Text: "x"}}}}
	var b strings.Builder
	if err := WriteCSV(&b, snap, time.Now()); err != nil || b.String() != "Age,Name,Status\nx,web,ok\n" {
		t.Errorf("CSV in the shown order: %q, %v", b.String(), err)
	}
}
