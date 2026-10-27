package kinds_test

import (
	"testing"

	"github.com/guided-traffic/cogpit/internal/model/kinds"
)

func FuzzLimitRanges(f *testing.F) { fuzzProfile(f, "limitranges") }

// TestLimitRangeTable pins the drawer's Limits table: a row per type and resource, the bounds in
// their columns.
func TestLimitRangeTable(t *testing.T) {
	p, _ := kinds.ByKey("limitranges")
	for _, c := range loadCases(t, "limitranges") {
		if c.name != "healthy" {
			continue
		}
		sections := p.Detail(c.obj)
		if len(sections) != 1 || sections[0].Table == nil {
			t.Fatalf("sections: %+v", sections)
		}
		tbl := sections[0].Table
		// Bytes in binary units (D-32); CPU and the ratio as written.
		want := [][]string{
			{"Container", "cpu", "10m", "2", "100m", "500m", "10"},
			{"Container", "memory", "16.0 MiB", "2.0 GiB", "128.0 MiB", "512.0 MiB", ""},
			{"Pod", "cpu", "", "4", "", "", ""},
			{"Pod", "memory", "", "4.0 GiB", "", "", ""},
			{"PersistentVolumeClaim", "storage", "1.0 GiB", "100.0 GiB", "", "", ""},
		}
		if len(tbl.Rows) != len(want) {
			t.Fatalf("rows: %q", tbl.Rows)
		}
		for i, row := range want {
			for j, cell := range row {
				if tbl.Rows[i][j] != cell {
					t.Errorf("row %d column %s = %q, want %q", i, tbl.Columns[j], tbl.Rows[i][j], cell)
				}
			}
		}
	}
}
