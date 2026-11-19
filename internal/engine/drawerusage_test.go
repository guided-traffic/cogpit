package engine

import (
	"testing"

	"github.com/guided-traffic/cogpit/internal/model/kinds"
)

// A drawer's poll asks for CPU and memory only while a visible chart shows them: a chart at its
// default shows CPU, or memory with CPU hidden; a variant hidden since shows the default; the
// containers' charts count unless hidden, and a pod the cache does not hold yet counts them at the
// default.
func TestDrawerUsageShownByItsCharts(t *testing.T) {
	pods, _ := kinds.ByKey("pods")
	nodes, _ := kinds.ByKey("nodes")
	for _, tc := range []struct {
		name     string
		nodes    bool
		variants map[string]string
		names    []string
		known    bool
		hidden   MetricSet
		want     bool
	}{
		{"a node at the default", true, nil, []string{""}, true, 0, true},
		{"a node's chart at the network", true, map[string]string{"": VariantNetwork}, []string{""}, true, 0, false},
		{"a node's chart at the filesystem", true, map[string]string{"": VariantFilesystem}, []string{""}, true, 0, false},
		{"a node's chart at memory", true, map[string]string{"": VariantMemory}, []string{""}, true, 0, true},
		{"a pod at the network, its container at the default", false, map[string]string{"": VariantNetwork}, []string{"", "app"}, true, 0, true},
		{"a pod and its container at the network and the filesystem", false, map[string]string{"": VariantNetwork, "app": VariantFilesystem}, []string{"", "app"}, true, 0, false},
		{"the containers' charts hidden", false, map[string]string{"": VariantNetwork}, []string{"", "app"}, true, HideContainers, false},
		{"a pod the cache does not hold yet", false, map[string]string{"": VariantNetwork}, []string{""}, false, 0, true},
		{"CPU and memory hidden", true, nil, []string{""}, true, HideCPU | HideMemory, false},
		{"CPU hidden: the default is memory", true, nil, []string{""}, true, HideCPU, true},
		{"the network hidden since it was chosen", true, map[string]string{"": VariantNetwork}, []string{""}, true, HideNetwork, true},
	} {
		d := &drawerWatch{backend: &Backend{}, profile: pods, variants: tc.variants, hidden: tc.hidden}
		if tc.nodes {
			d.profile = nodes
		}
		if got := d.usageShownLocked(tc.names, tc.known); got != tc.want {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
}
