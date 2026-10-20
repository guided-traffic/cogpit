package commands_test

import (
	"fmt"
	"slices"
	"testing"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// TestResourcesListPagesThroughEveryNamespace (the side finding of the gate walk's GW9, D-106): a
// resources.list without a limit answers 100 rows, so the namespace filter, which asked so, offered
// the first 100 namespaces of a larger cluster; with offset every row is read page by page, in the
// name order, each row once.
func TestResourcesListPagesThroughEveryNamespace(t *testing.T) {
	src := seed()
	for i := range 150 {
		src.Set(testkit.NamespacesGVR, testkit.Object("v1", "Namespace", "", fmt.Sprintf("team-%03d", i), nil))
	}
	h := newHarness(t, src, engine.Options{})
	agent := engine.OriginExternalAgent
	const total = 152 // infra, shop and the 150 teams

	var list commands.ListResult
	h.must(t, agent, "resources.list", map[string]any{"kind": "namespaces"}, &list)
	if list.Total != total || list.Returned != 100 {
		t.Fatalf("without a limit: total %d, returned %d; want %d and 100 (the filter's old request)", list.Total, list.Returned, total)
	}

	read := func(limit int) []string {
		var names []string
		for offset := 0; ; {
			var page commands.ListResult
			h.must(t, agent, "resources.list", map[string]any{"kind": "namespaces", "limit": limit, "offset": offset}, &page)
			if page.Total != total || page.Returned != len(page.Rows) {
				t.Fatalf("offset %d: total %d, returned %d of %d rows", offset, page.Total, page.Returned, len(page.Rows))
			}
			for _, r := range page.Rows {
				names = append(names, r.Ref.Name)
			}
			offset += page.Returned
			if page.Returned == 0 || offset >= page.Total {
				return names
			}
		}
	}
	all := read(1000)
	if len(all) != total || !slices.IsSorted(all) || len(slices.Compact(slices.Clone(all))) != total {
		t.Fatalf("one page of 1000: %d names, sorted %v", len(all), slices.IsSorted(all))
	}
	if paged := read(40); !slices.Equal(paged, all) {
		t.Fatalf("pages of 40 read %d names, not the %d of one page", len(paged), len(all))
	}

	h.must(t, agent, "resources.list", map[string]any{"kind": "namespaces", "offset": 500}, &list)
	if list.Total != total || list.Returned != 0 || len(list.Rows) != 0 {
		t.Errorf("an offset past the end: %+v", list)
	}
	if err := h.run(t, agent, "resources.list", map[string]any{"kind": "namespaces", "offset": -1}, nil); err == nil {
		t.Error("a negative offset accepted")
	}
}
