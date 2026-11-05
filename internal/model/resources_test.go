package model_test

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/model"
)

func podWith(containers ...map[string]any) *unstructured.Unstructured {
	list := make([]any, len(containers))
	for i, c := range containers {
		list[i] = c
	}
	return &unstructured.Unstructured{Object: map[string]any{"spec": map[string]any{"containers": list}}}
}

func container(name string, req, lim map[string]any) map[string]any {
	res := map[string]any{}
	if req != nil {
		res["requests"] = req
	}
	if lim != nil {
		res["limits"] = lim
	}
	return map[string]any{"name": name, "resources": res}
}

func TestPodResourcesAndTotals(t *testing.T) {
	pod := podWith(
		container("app", map[string]any{"cpu": "250m", "memory": "64Mi"}, map[string]any{"cpu": "1", "memory": "128Mi"}),
		container("sidecar", map[string]any{"cpu": "50m"}, map[string]any{"cpu": "100m"}), // no memory limit
		map[string]any{"name": ""}, // ignored
	)
	rs := model.PodResources(pod)
	if len(rs) != 2 || rs[0].Name != "app" || rs[0].Requests.CPUMilli != 250 || rs[0].Limits.MemoryBytes != 128<<20 {
		t.Fatalf("resources = %+v", rs)
	}
	req, lim := model.PodTotals(rs)
	if req.CPUMilli != 300 || req.MemoryBytes != 64<<20 {
		t.Errorf("requests = %+v", req)
	}
	if lim.CPUMilli != 1100 {
		t.Errorf("cpu limit = %d, want the sum 1100 (every container sets one)", lim.CPUMilli)
	}
	if lim.MemoryBytes != 0 {
		t.Errorf("memory limit = %d, want none: one container has no memory limit, so the pod's memory is unbounded", lim.MemoryBytes)
	}
	if req, lim := model.PodTotals(nil); req != (model.Usage{}) || lim != (model.Usage{}) {
		t.Error("a pod without containers has no lines")
	}
}

func TestNodeCapacity(t *testing.T) {
	node := &unstructured.Unstructured{Object: map[string]any{"status": map[string]any{
		"allocatable": map[string]any{"cpu": "3920m", "memory": "7900Mi"},
		"capacity":    map[string]any{"cpu": "4", "memory": "8Gi"},
	}}}
	a, c := model.NodeCapacity(node)
	if a.CPUMilli != 3920 || a.MemoryBytes != 7900<<20 || c.CPUMilli != 4000 || c.MemoryBytes != 8<<30 {
		t.Errorf("allocatable %+v, capacity %+v", a, c)
	}
}

func TestParseQuantityRefusesWhatSpendsUnboundedTime(t *testing.T) {
	for _, s := range []string{"", "1e999999999", "12e-99", "x", "1Zi2"} {
		if _, ok := model.ParseQuantity(s); ok {
			t.Errorf("ParseQuantity(%q) accepted", s)
		}
	}
	if q, ok := model.ParseQuantity("1.5"); !ok || q.MilliValue() != 1500 {
		t.Errorf("ParseQuantity(1.5) = %v, %v", q, ok)
	}
	// Hostile content in the spec is zero, not a panic.
	pod := podWith(container("a", map[string]any{"cpu": "1e999999999", "memory": 5}, nil))
	if rs := model.PodResources(pod); len(rs) != 1 || rs[0].Requests.CPUMilli != 0 {
		t.Errorf("hostile resources = %+v", rs)
	}
}

// A pod's allocation as Kubernetes counts it: the containers with the sidecars, at least an init
// container beside the sidecars started before it, the overhead (on a limit only where one is
// set), the pod-level resources over all of it; a limit is open when a container or a sidecar
// runs without one.
func TestPodAllocated(t *testing.T) {
	res := func(cpu, mem string) map[string]any {
		m := map[string]any{}
		if cpu != "" {
			m["cpu"] = cpu
		}
		if mem != "" {
			m["memory"] = mem
		}
		return m
	}
	ctr := func(req, lim map[string]any, extra ...string) map[string]any {
		c := map[string]any{"name": "c", "resources": map[string]any{"requests": req, "limits": lim}}
		if len(extra) > 0 {
			c["restartPolicy"] = extra[0]
		}
		return c
	}
	pod := func(spec map[string]any) *unstructured.Unstructured {
		return &unstructured.Unstructured{Object: map[string]any{"spec": spec}}
	}
	for _, tc := range []struct {
		name               string
		spec               map[string]any
		req, lim           model.Usage
		openCPU, openMemor bool
	}{
		{"two containers", map[string]any{"containers": []any{
			ctr(res("250m", "64Mi"), res("500m", "128Mi")), ctr(res("250m", "64Mi"), res("1", "128Mi"))}},
			model.Usage{CPUMilli: 500, MemoryBytes: 128 << 20}, model.Usage{CPUMilli: 1500, MemoryBytes: 256 << 20}, false, false},
		{"one without limits", map[string]any{"containers": []any{
			ctr(res("250m", "64Mi"), res("500m", "128Mi")), ctr(res("100m", "32Mi"), res("", ""))}},
			model.Usage{CPUMilli: 350, MemoryBytes: 96 << 20}, model.Usage{CPUMilli: 500, MemoryBytes: 128 << 20}, true, true},
		{"a big init container", map[string]any{
			"initContainers": []any{ctr(res("2", "1Gi"), res("2", "1Gi"))},
			"containers":     []any{ctr(res("500m", "256Mi"), res("1", "512Mi"))}},
			model.Usage{CPUMilli: 2000, MemoryBytes: 1 << 30}, model.Usage{CPUMilli: 2000, MemoryBytes: 1 << 30}, false, false},
		{"a sidecar adds, an init container beside it", map[string]any{
			"initContainers": []any{
				ctr(res("100m", "64Mi"), res("200m", "128Mi"), "Always"),
				ctr(res("1", "256Mi"), res("1", "256Mi"))},
			"containers": []any{ctr(res("500m", "256Mi"), res("1", "512Mi"))}},
			model.Usage{CPUMilli: 1100, MemoryBytes: 320 << 20}, model.Usage{CPUMilli: 1200, MemoryBytes: 640 << 20}, false, false},
		{"a sidecar without limits opens them", map[string]any{
			"initContainers": []any{ctr(res("100m", "64Mi"), res("", ""), "Always")},
			"containers":     []any{ctr(res("500m", "256Mi"), res("1", "512Mi"))}},
			model.Usage{CPUMilli: 600, MemoryBytes: 320 << 20}, model.Usage{CPUMilli: 1000, MemoryBytes: 512 << 20}, true, true},
		{"overhead", map[string]any{
			"overhead":   res("50m", "16Mi"),
			"containers": []any{ctr(res("500m", "256Mi"), res("1", ""))}},
			model.Usage{CPUMilli: 550, MemoryBytes: 272 << 20}, model.Usage{CPUMilli: 1050}, false, true},
		{"pod-level resources", map[string]any{
			"resources":  map[string]any{"requests": res("2", ""), "limits": res("", "1Gi")},
			"containers": []any{ctr(res("500m", "256Mi"), res("1", ""))}},
			model.Usage{CPUMilli: 2000, MemoryBytes: 256 << 20}, model.Usage{CPUMilli: 1000, MemoryBytes: 1 << 30}, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := model.PodAllocated(pod(tc.spec))
			if a.Requests != tc.req || a.Limits != tc.lim || a.OpenCPU != tc.openCPU || a.OpenMemory != tc.openMemor {
				t.Errorf("got %+v, want requests %+v limits %+v open %v/%v", a, tc.req, tc.lim, tc.openCPU, tc.openMemor)
			}
		})
	}
}
