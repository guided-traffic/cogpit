package kinds

import (
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/model"
)

// A namespace's usage and allocation (D-154): the CPU and Memory its pods use, summed from the
// Metrics API's one list of every pod (model.UsageNamespaces), and what its scheduled, unfinished
// pods request, with their limits, summed from the pods' cache by PodsByNamespace: the requests
// in the text's colour, the limits muted after them, "750m / 1.5" and "41.1 / 123.4 GiB". A limit
// some pod runs without is open: "4+", the "+" orange for memory (a container without a memory
// limit can take a node's memory), muted with the limits for CPU (a common choice that only
// shares spare CPU). A namespace whose pods are not listed shows nothing, never 0.

// The positions of PodsByNamespace's totals.
const (
	totCPURequest = iota
	totMemoryRequest
	totCPULimit
	totMemoryLimit
	totOpenCPU    // pods without a CPU limit
	totOpenMemory // pods without a memory limit
	totPods       // pods counted
)

// The ids and titles of the Namespaces list's allocation columns.
const (
	idCPURequests       = "cpuRequests"
	idMemoryRequests    = "memoryRequests"
	titleCPURequests    = "CPU Req / Lim"
	titleMemoryRequests = "Memory Req / Lim"
)

var (
	cpuRequestsColumn    = model.Column{ID: idCPURequests, Title: titleCPURequests, Type: model.ColumnMetric}
	memoryRequestsColumn = model.Column{ID: idMemoryRequests, Title: titleMemoryRequests, Type: model.ColumnMetric}
)

func podNamespaceKeys(pod *unstructured.Unstructured) []string {
	if ns := pod.GetNamespace(); ns != "" {
		return []string{ns}
	}
	return nil
}

// podAllocationTotals is what a pod adds to its namespace: its allocation (model.PodAllocated)
// once it is scheduled and until it has finished, as kubectl's "Allocated resources" counts; a
// pod waiting for a node or done adds nothing.
func podAllocationTotals(pod *unstructured.Unstructured) model.Totals {
	var t model.Totals
	phase := model.NestedString(pod.Object, "status", "phase")
	if model.NestedString(pod.Object, "spec", "nodeName") == "" || phase == podSucceeded || phase == podFailed {
		return t
	}
	a := model.PodAllocated(pod)
	t[totCPURequest], t[totMemoryRequest] = a.Requests.CPUMilli, a.Requests.MemoryBytes
	t[totCPULimit], t[totMemoryLimit] = a.Limits.CPUMilli, a.Limits.MemoryBytes
	if a.OpenCPU {
		t[totOpenCPU] = 1
	}
	if a.OpenMemory {
		t[totOpenMemory] = 1
	}
	t[totPods] = 1
	return t
}

// namespaceAllocationCells are a namespace's CPU and Memory Req / Lim cells; empty while its pods
// are not listed (not yet, or not readable).
func namespaceAllocationCells(obj *unstructured.Unstructured, rel model.Related) (cpu, memory model.Cell) {
	t, ok := model.TotalsOf(rel, PodsByNamespace, obj.GetName())
	if !ok {
		return model.Cell{}, model.Cell{}
	}
	cpu = allocationCell(model.FormatCPU(t[totCPURequest]), model.FormatCPU(t[totCPULimit]), t[totCPURequest], t[totOpenCPU], t[totPods], "CPU", model.StatusNone)
	memRequests, memLimits := unitOnce(model.FormatBytes(t[totMemoryRequest]), model.FormatBytes(t[totMemoryLimit]))
	memory = allocationCell(memRequests, memLimits, t[totMemoryRequest], t[totOpenMemory], t[totPods], "memory", model.StatusWarning)
	return cpu, memory
}

// unitOnce writes byte amounts of the same unit with the unit once, after the limits: "41.1" and
// "123.4 GiB"; amounts in different units keep theirs, "768.0 MiB" and "3.8 GiB".
func unitOnce(requests, limits string) (string, string) {
	number, unit, _ := strings.Cut(requests, " ")
	if _, limitUnit, _ := strings.Cut(limits, " "); unit == limitUnit {
		return number, limits
	}
	return requests, limits
}

// allocationCell is "<requests> / <limits>", the limits muted, with a "+" after them when open of
// the pods run without that limit: muted with the limits for StatusNone, else glued in plus's
// status colour. The requests are the sort key. Its tooltip says how many pods count and how many
// run without the limit.
func allocationCell(requests, limits string, sortKey, open, pods int64, what string, plus model.Status) model.Cell {
	tip := pluralPods(pods) + " scheduled and not finished"
	req, lim := model.CellPart{Text: requests}, model.CellPart{Text: "/ " + limits, Muted: true}
	var c model.Cell
	switch {
	case open == 0:
		c = model.PartsCell(model.StatusNone, req, lim)
	case plus == model.StatusNone:
		lim.Text += "+"
		c = model.PartsCell(model.StatusNone, req, lim)
	default:
		c = model.PartsCell(model.StatusNone, req, lim, model.CellPart{Text: "+", Status: plus, Glued: true})
	}
	if open > 0 {
		tip += "; " + pluralPods(open) + " without a " + what + " limit: the limits' sum is open above"
	}
	c.Num, c.Tip = sortKey, tip
	return c
}

func pluralPods(n int64) string {
	if n == 1 {
		return "1 pod"
	}
	return strconv.FormatInt(n, 10) + " pods"
}
