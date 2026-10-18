// Package metrics finds out where a cluster's resource usage comes from and polls it while
// something visible shows it (architecture §9).
//
// A [Provider] answers "what do these pods and nodes use right now". There are two: the
// Kubernetes Metrics Server ([Server], the metrics.k8s.io API) and Prometheus through the API
// server's service proxy, which also answers for the past (range queries). A [Detector] finds a provider
// (and says why it found none), a [Hub] polls it while subscribed and keeps the latest results and
// a short time series per watched object. Metrics cannot be watched, so polling is the documented
// exception to "never poll what can be watched" (AGENTS.md, cache first), and it happens only
// while a view or a drawer that shows metrics is open.
//
// A missing API, a missing permission and an unreachable metrics-server are [State]s, not errors:
// the UI shows them and the lists stay blank.
//
// Layer L2: it talks to the API server, through the cluster's counting transport.
package metrics

import (
	"context"
	"errors"
	"time"

	"github.com/guided-traffic/cogpit/internal/model"
)

// Status says whether a cluster has usable metrics.
type Status string

// Statuses.
const (
	// StatusUnknown: nothing asked yet.
	StatusUnknown Status = "unknown"
	// StatusAvailable: a provider answers.
	StatusAvailable Status = "available"
	// StatusNotServed: the cluster serves no metrics API (no metrics-server installed).
	StatusNotServed Status = "notServed"
	// StatusForbidden: the API exists, but the user may not read it.
	StatusForbidden Status = "forbidden"
	// StatusUnavailable: the API is registered but fails (metrics-server down or not ready), or
	// the request failed for another reason.
	StatusUnavailable Status = "unavailable"
)

// State is what is known about a cluster's metrics: which provider, or why none.
type State struct {
	Status Status `json:"status"`
	// Provider is the provider's id ("metrics-server") when Status is available.
	Provider string `json:"provider,omitempty"`
	// Label is the provider as Lens names it in its source line: "Kubernetes Metrics Server".
	Label string `json:"label,omitempty"`
	// Message says why metrics are not available, in a sentence the UI shows.
	Message string `json:"message,omitempty"`
	// Reason is the failure's class and short reason ("unavailable (HTTP 503)", "timeout"), the
	// status line's last error; empty when nothing failed (a provider that is simply not installed
	// has none).
	Reason string `json:"reason,omitempty"`
}

// Usable reports whether a provider answers.
func (s State) Usable() bool { return s.Status == StatusAvailable }

// Capabilities say what a provider can draw. The Metrics Server gives the current CPU and memory
// only; Prometheus will add history (a time range of its own), network and filesystem.
type Capabilities struct {
	// History: the provider answers range queries, so a chart needs no series kept in memory.
	History bool `json:"history,omitempty"`
	// Network and Filesystem: the provider has these variants.
	Network    bool `json:"network,omitempty"`
	Filesystem bool `json:"filesystem,omitempty"`
}

// ErrNoData means the provider has no measurement for the object yet (a pod that started a moment
// ago, a node the metrics-server has not scraped): not an error state of the cluster's metrics.
var ErrNoData = errors.New("no metrics for the object yet")

// Provider reads current usage. Implementations are safe for concurrent use.
type Provider interface {
	// ID is the provider's stable id: "metrics-server".
	ID() string
	// Label is the provider as the UI names it: "Kubernetes Metrics Server".
	Label() string
	// Capabilities say which variants the provider can draw.
	Capabilities() Capabilities
	// ListPods returns the usage of the pods of a namespace, or of all namespaces for "".
	ListPods(ctx context.Context, namespace string) ([]model.PodUsage, error)
	// GetPod returns one pod's usage, or ErrNoData.
	GetPod(ctx context.Context, namespace, name string) (model.PodUsage, error)
	// ListNodes returns the usage of every node.
	ListNodes(ctx context.Context) ([]model.NodeUsage, error)
	// GetNode returns one node's usage, or ErrNoData.
	GetNode(ctx context.Context, name string) (model.NodeUsage, error)
}

// Detector finds a cluster's provider. It returns the provider when the state is available, else
// nil and the state that says why (not served, forbidden, unavailable). It makes its own requests
// (discovery) and is asked rarely: the hub keeps the answer for a while.
type Detector interface {
	Detect(ctx context.Context) (Provider, State)
}

// DetectorFunc adapts a function to a Detector.
type DetectorFunc func(ctx context.Context) (Provider, State)

// Detect calls f.
func (f DetectorFunc) Detect(ctx context.Context) (Provider, State) { return f(ctx) }

// Fixed is a Detector that always finds p: the synthetic demo cluster's.
func Fixed(p Provider) Detector { return fixedDetector{p} }

// fixedDetector is Fixed's: the hub knows its provider before it asks (Hub.DisksPossible).
type fixedDetector struct{ p Provider }

// Detect implements Detector.
func (f fixedDetector) Detect(context.Context) (Provider, State) {
	return f.p, State{Status: StatusAvailable, Provider: f.p.ID(), Label: f.p.Label()}
}

// msgCancelled is the state message of a request that was cancelled: it says nothing about the
// cluster.
const msgCancelled = "the request was cancelled"

// The filesystem metrics a series can come from (ObjectSeries.FilesystemSource).
const (
	// FilesystemUsage: bytes in use (container_fs_usage_bytes).
	FilesystemUsage = "usage"
	// FilesystemIO: read and write rates, when the cluster has no usage metric.
	FilesystemIO = "io"
)

// StateError is a provider error that already says which state it means (a forbidden service
// proxy, a Prometheus that went away); the hub uses the state as it is instead of guessing from
// the error's text.
type StateError struct {
	State State
	Err   error
}

// Error implements error.
func (e *StateError) Error() string {
	if e.Err != nil {
		return e.State.Message + ": " + e.Err.Error()
	}
	return e.State.Message
}

// Unwrap returns the underlying error.
func (e *StateError) Unwrap() error { return e.Err }

// RangeQuery asks a history provider for an object's series between From and To, one point per
// Step. Kind is KindPods or KindNodes; Namespace is empty for a node. Want says which series; zero
// asks for all of them. Addresses are the addresses of the node (or of the cluster's control-plane
// nodes) besides their names (Hub.SetNodeAddresses): a Prometheus whose series name a node by
// its address as the instance (kube-prometheus-stack's node-exporter) is matched by them.
type RangeQuery struct {
	Kind      Kind
	Namespace string
	Name      string
	Addresses []string
	From, To  time.Time
	Step      time.Duration
	Want      Want
}

// Want is a set of the series a range query asks for. The hub asks for what the open drawer's
// charts show: CPU and memory always (one variant switch must not wait for a request), the network
// and the filesystem only while a chart shows them.
type Want uint8

// The series of a range query.
const (
	// WantUsage: CPU and memory (two queries).
	WantUsage Want = 1 << iota
	// WantNetwork: the bytes received and transmitted (two queries).
	WantNetwork
	// WantFilesystem: the filesystem's usage, or its read and write rates (one or two queries).
	WantFilesystem
	// WantPods: the cluster's pods on its nodes, from kube-state-metrics (the cluster scope only;
	// one query, two with control-plane nodes). Asked for over a chart's range only: the cache
	// counts the pods after that (promcluster.go).
	WantPods
	// WantAll is every series of a pod or a node.
	WantAll = WantUsage | WantNetwork | WantFilesystem
)

// Has reports whether w includes every series of o.
func (w Want) Has(o Want) bool { return w&o == o }

// TargetSeries are the series of one chart: the object as a whole (key "" of ObjectSeries), or one
// container of a pod.
type TargetSeries struct {
	// Usage: CPU and memory, in the units of the Metrics Server's measurements, oldest first.
	Usage []model.UsageSample `json:"usage,omitempty"`
	// Network is set for the whole object only: the network is a pod's, not a container's.
	Network    []model.NetworkSample    `json:"network,omitempty"`
	Filesystem []model.FilesystemSample `json:"filesystem,omitempty"`
	// Pods is the cluster scope's pod count of the target's nodes (WantPods).
	Pods []model.CountSample `json:"pods,omitempty"`
}

// ObjectSeries is the answer to a RangeQuery.
type ObjectSeries struct {
	// Targets: "" is the pod or node as a whole, then one entry per container that has series.
	Targets map[string]*TargetSeries `json:"targets"`
	// FilesystemSource says which metric the Filesystem series come from: "usage" (bytes in use),
	// "io" (read and write rates, used when the cluster's Prometheus has no usage series), or ""
	// when there are no filesystem series.
	FilesystemSource string `json:"filesystemSource,omitempty"`
	// PodsSource says where the Pods series come from once they were asked for (WantPods):
	// "kube-state-metrics", or "none" when the Prometheus has no such series; "" when not asked.
	PodsSource string `json:"podsSource,omitempty"`
	// UsageSource says where the CPU and memory series come from once they were asked for
	// (WantUsage): UsageNodeExporter (a node, the cluster), UsageCAdvisor, or UsageNone when the
	// Prometheus has no such series over the range; "" when not asked.
	UsageSource string `json:"usageSource,omitempty"`
	// From, To and Step are the grid the series were asked for.
	From, To time.Time
	Step     time.Duration
}

// RangeProvider is a Provider with history (Capabilities.History): it answers range queries, so
// the hub keeps no series of its own for it. Implementations are safe for concurrent use.
type RangeProvider interface {
	Provider
	// ObjectRange returns the series of one pod or node. An object with no series is an empty
	// result, not an error.
	ObjectRange(ctx context.Context, q RangeQuery) (*ObjectSeries, error)
}
