package metrics

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// TestExporterMemoryIsTheKubeletsWorkingSet (D-new): node-exporter's memory, as exporterMemory
// computes it, is the working set the kubelet reports for the node (the Metrics Server's live
// value), so a node's history does not step where the live values continue it. The fixtures are
// synthetic nodes shaped like a real cluster's: /proc/meminfo, which node-exporter exports, and the
// root cgroup's memory.stat (cgroup v2), which the kubelet reads; the kernel memory (slab, stacks,
// page tables) the kubelet leaves out is 0.68 GiB of a 3.7 GiB worker and 19 GiB of a master.
func TestExporterMemoryIsTheKubeletsWorkingSet(t *testing.T) {
	const gib = 1 << 30
	for _, tc := range []struct {
		node        string
		kubelet     float64 // the working set, GiB
		withoutFree float64 // MemTotal − MemFree − Inactive(file), GiB
	}{
		{"worker", 2.14, 2.82},
		{"master", 80, 99},
	} {
		t.Run(tc.node, func(t *testing.T) {
			series := nodeExporterMeminfo(t, filepath.Join("testdata", "nodememory", tc.node+".meminfo"))
			stat := memoryStat(t, filepath.Join("testdata", "nodememory", tc.node+".memory.stat"))

			// The kubelet's node is cAdvisor's root cgroup. On cgroup v2 the root has no
			// memory.current: runc's usage is memory.stat's anon + file, cAdvisor's working set
			// that usage less inactive_file.
			kubelet := stat["anon"] + stat["file"] - stat["inactive_file"]
			if got := kubelet / gib; got < tc.kubelet-0.005 || got > tc.kubelet+0.005 {
				t.Fatalf("the fixture's working set is %.3f GiB, want %.2f", got, tc.kubelet)
			}

			for name, q := range map[string]string{"exporterMemory": exporterMemory(""), "exporterGroupMemory": exporterGroupMemory("")} {
				if got := evalExporterSum(t, q, series); got != kubelet {
					t.Errorf("%s: %.3f GiB from node-exporter, the kubelet's working set is %.3f GiB", name, got/gib, kubelet/gib)
				}
			}
			old := series["node_memory_MemTotal_bytes"] - series["node_memory_MemFree_bytes"] - series["node_memory_Inactive_file_bytes"]
			if got := old / gib; got < tc.withoutFree-0.005 || got > tc.withoutFree+0.005 {
				t.Errorf("MemTotal − MemFree − Inactive(file) is %.3f GiB, want the fixture's %.2f", got, tc.withoutFree)
			}
		})
	}
}

// nodeExporterMeminfo reads a /proc/meminfo as node-exporter's meminfo collector exports it: a
// field "Inactive(file)" becomes node_memory_Inactive_file_bytes, a value in kB is multiplied by
// 1024.
func nodeExporterMeminfo(t *testing.T, path string) map[string]float64 {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	paren := regexp.MustCompile(`\((.*)\)`)
	out := map[string]float64{}
	for line := range strings.Lines(string(b)) {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		v, err := strconv.ParseFloat(f[1], 64)
		if err != nil {
			t.Fatalf("%s: %q", path, line)
		}
		if len(f) == 3 && f[2] == "kB" {
			v *= 1024
		}
		out["node_memory_"+paren.ReplaceAllString(strings.TrimSuffix(f[0], ":"), "_${1}")+"_bytes"] = v
	}
	return out
}

// memoryStat reads a cgroup's memory.stat.
func memoryStat(t *testing.T, path string) map[string]float64 {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]float64{}
	for line := range strings.Lines(string(b)) {
		k, v, ok := strings.Cut(strings.TrimSpace(line), " ")
		n, err := strconv.ParseFloat(v, 64)
		if !ok || err != nil {
			t.Fatalf("%s: %q", path, line)
		}
		out[k] = n
	}
	return out
}

// evalExporterSum evaluates exporterMemory's form, sum(max by (node, instance) (a + b … - z)), for
// one exporter: every series it names must be one node-exporter exports.
func evalExporterSum(t *testing.T, q string, series map[string]float64) float64 {
	t.Helper()
	inner, prefixed := strings.CutPrefix(q, "sum(max by (node, instance) (")
	inner, suffixed := strings.CutSuffix(inner, "))")
	if !prefixed || !suffixed {
		t.Fatalf("not exporterMemory's form: %s", q)
	}
	terms := strings.Fields(inner)
	sum, sign := 0.0, 1.0
	for i, term := range terms {
		if i%2 == 1 {
			switch term {
			case "+":
				sign = 1
			case "-":
				sign = -1
			default:
				t.Fatalf("operator %q in %s", term, q)
			}
			continue
		}
		v, ok := series[term]
		if !ok {
			t.Fatalf("%s is no series node-exporter exports from /proc/meminfo", term)
		}
		sum += sign * v
	}
	return sum
}
