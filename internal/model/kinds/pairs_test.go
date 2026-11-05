package kinds

import (
	"maps"
	"slices"
	"testing"

	"github.com/guided-traffic/cogpit/internal/model"
)

// The columns of labels and of selectors say so (Free width's exceptions match them) and are
// hidden by default: such lists are read in the drawer (D-153).
func TestPairsColumnsHiddenByDefault(t *testing.T) {
	want := map[string]string{
		"namespaces/labels":               model.PairsLabels,
		"secrets/labels":                  model.PairsLabels,
		"services/selector":               model.PairsSelector,
		"replicationcontrollers/selector": model.PairsSelector,
		"daemonsets/nodeSelector":         model.PairsSelector,
	}
	got := map[string]string{}
	for _, p := range Builtin() {
		for _, c := range p.Columns {
			if c.Pairs == "" {
				continue
			}
			got[p.Key+"/"+c.ID] = c.Pairs
			if !c.Hidden {
				t.Errorf("%s/%s is shown by default", p.Key, c.ID)
			}
		}
	}
	if !maps.Equal(got, want) {
		t.Errorf("columns of pairs %v, want %v", slices.Sorted(maps.Keys(got)), slices.Sorted(maps.Keys(want)))
	}
}
