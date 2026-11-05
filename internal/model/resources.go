package model

import (
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// UsageKind says which Metrics API objects a kind's CPU and Memory columns show.
type UsageKind string

// Kinds with usage columns.
const (
	UsagePods  UsageKind = "pods"
	UsageNodes UsageKind = "nodes"
	// UsageNamespaces is a namespace's pods' usage summed: the Metrics API's list of every pod.
	UsageNamespaces UsageKind = "namespaces"
)

// Charted reports whether a drawer charts a usage kind's object: a pod's or a node's own
// measurements. A namespace's usage is a list's sum, without a drawer's charts.
func (k UsageKind) Charted() bool { return k == UsagePods || k == UsageNodes }

// The ids of the usage columns of a kind with a [KindProfile.Usage].
const (
	ColumnCPU    = "cpu"
	ColumnMemory = "memory"
	// ColumnDisk is the Nodes list's Disk column ([KindProfile.DiskCell]): the root filesystem,
	// from Prometheus's node-exporter series.
	ColumnDisk = "disk"
)

// ParseQuantity parses a Kubernetes resource quantity ("250m", "64Mi", "1.5") with a guard: it
// refuses input longer than any quantity and decimal exponents beyond ±30, which
// resource.ParseQuantity spends unbounded time on.
func ParseQuantity(s string) (resource.Quantity, bool) {
	if s == "" || len(s) > 64 {
		return resource.Quantity{}, false
	}
	if i := strings.IndexAny(s, "eE"); i >= 0 {
		if rest := s[i+1:]; rest != "" && rest != "i" { // "E" and "Ei" are suffixes
			exp, err := strconv.Atoi(rest)
			if err != nil || exp < -30 || exp > 30 {
				return resource.Quantity{}, false
			}
		}
	}
	q, err := resource.ParseQuantity(s)
	return q, err == nil
}

// UsageOf reads a resource list ({"cpu": "250m", "memory": "64Mi"}) at the given path of an
// object as a Usage; a missing or unparseable entry is 0 ("not set").
func UsageOf(root any, fields ...string) Usage {
	list := NestedMap(root, fields...)
	var u Usage
	if q, ok := ParseQuantity(NestedIntOrString(list, "cpu")); ok {
		u.CPUMilli = q.MilliValue()
	}
	if q, ok := ParseQuantity(NestedIntOrString(list, "memory")); ok {
		u.MemoryBytes = q.Value()
	}
	return u
}

// ContainerResources are one container's CPU and memory requests and limits; 0 means not set.
type ContainerResources struct {
	Name     string `json:"name"`
	Requests Usage  `json:"requests"`
	Limits   Usage  `json:"limits"`
}

// PodResources returns the requests and limits of a pod's containers (spec.containers; init
// containers are not measured by the Metrics API), in spec order.
func PodResources(pod *unstructured.Unstructured) []ContainerResources {
	var out []ContainerResources
	for _, c := range NestedSlice(pod.Object, "spec", "containers") {
		name := NestedString(c, "name")
		if name == "" {
			continue
		}
		out = append(out, ContainerResources{
			Name:     name,
			Requests: UsageOf(c, "resources", "requests"),
			Limits:   UsageOf(c, "resources", "limits"),
		})
	}
	return out
}

// PodTotals returns what the pod as a whole requests and may use: the sum of its containers'
// requests, and the sum of their limits only when every container sets that limit (a container
// without a CPU limit makes the pod's CPU unbounded: no line to draw), for CPU and memory
// separately.
func PodTotals(cs []ContainerResources) (requests, limits Usage) {
	cpuLimited, memLimited := len(cs) > 0, len(cs) > 0
	for _, c := range cs {
		requests = requests.Add(c.Requests)
		limits = limits.Add(c.Limits)
		cpuLimited = cpuLimited && c.Limits.CPUMilli > 0
		memLimited = memLimited && c.Limits.MemoryBytes > 0
	}
	if !cpuLimited {
		limits.CPUMilli = 0
	}
	if !memLimited {
		limits.MemoryBytes = 0
	}
	return requests, limits
}

// NodeCapacity returns a node's allocatable and capacity (status.allocatable, status.capacity).
func NodeCapacity(node *unstructured.Unstructured) (allocatable, capacity Usage) {
	return UsageOf(node.Object, "status", "allocatable"), UsageOf(node.Object, "status", "capacity")
}

// PodAllocation is what a pod takes from its node as the scheduler counts it (kubectl describe
// node's "Allocated resources"): its requests and limits, init containers, sidecars and its
// overhead included; OpenCPU and OpenMemory say that a container runs without that limit, so
// that the pod may use more than Limits says.
type PodAllocation struct {
	Requests, Limits    Usage
	OpenCPU, OpenMemory bool
}

// PodAllocated returns a pod's allocation as Kubernetes computes it (component-helpers' PodRequests
// and PodLimits): the containers' sum with the sidecars' (init containers with restartPolicy
// Always), at least what any init container needs beside the sidecars started before it, plus
// spec.overhead (on a limit only where one is set); spec.resources, the pod-level resources,
// replaces a resource it names. A limit is open when a container or a sidecar sets none and the
// pod sets none; an init container's own only holds while it runs.
func PodAllocated(pod *unstructured.Unstructured) PodAllocation {
	var a PodAllocation
	var sidecarReq, sidecarLim, initReq, initLim Usage
	for _, c := range NestedSlice(pod.Object, "spec", "initContainers") {
		req, lim := UsageOf(c, "resources", "requests"), UsageOf(c, "resources", "limits")
		if NestedString(c, "restartPolicy") == "Always" {
			sidecarReq, sidecarLim = sidecarReq.Add(req), sidecarLim.Add(lim)
			initReq, initLim = maxEach(initReq, sidecarReq), maxEach(initLim, sidecarLim)
			a.OpenCPU, a.OpenMemory = a.OpenCPU || lim.CPUMilli == 0, a.OpenMemory || lim.MemoryBytes == 0
			continue
		}
		initReq, initLim = maxEach(initReq, req.Add(sidecarReq)), maxEach(initLim, lim.Add(sidecarLim))
	}
	for _, c := range NestedSlice(pod.Object, "spec", "containers") {
		lim := UsageOf(c, "resources", "limits")
		a.Requests, a.Limits = a.Requests.Add(UsageOf(c, "resources", "requests")), a.Limits.Add(lim)
		a.OpenCPU, a.OpenMemory = a.OpenCPU || lim.CPUMilli == 0, a.OpenMemory || lim.MemoryBytes == 0
	}
	a.Requests, a.Limits = maxEach(a.Requests.Add(sidecarReq), initReq), maxEach(a.Limits.Add(sidecarLim), initLim)
	over := UsageOf(pod.Object, "spec", "overhead")
	a.Requests = a.Requests.Add(over)
	if a.Limits.CPUMilli > 0 {
		a.Limits.CPUMilli += over.CPUMilli
	}
	if a.Limits.MemoryBytes > 0 {
		a.Limits.MemoryBytes += over.MemoryBytes
	}
	podReq, podLim := UsageOf(pod.Object, "spec", "resources", "requests"), UsageOf(pod.Object, "spec", "resources", "limits")
	if podReq.CPUMilli > 0 {
		a.Requests.CPUMilli = podReq.CPUMilli
	}
	if podReq.MemoryBytes > 0 {
		a.Requests.MemoryBytes = podReq.MemoryBytes
	}
	if podLim.CPUMilli > 0 {
		a.Limits.CPUMilli, a.OpenCPU = podLim.CPUMilli, false
	}
	if podLim.MemoryBytes > 0 {
		a.Limits.MemoryBytes, a.OpenMemory = podLim.MemoryBytes, false
	}
	return a
}

// maxEach is the larger of a and b, CPU and memory each.
func maxEach(a, b Usage) Usage {
	return Usage{CPUMilli: max(a.CPUMilli, b.CPUMilli), MemoryBytes: max(a.MemoryBytes, b.MemoryBytes)}
}
