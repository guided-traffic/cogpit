package engine

import (
	"github.com/guided-traffic/cogpit/internal/cache"
	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/model"
)

// DetailModel is what the detail drawer shows for one object, built from the cache. It is also
// MCP's object_get result, so its JSON shape is kept stable: fields are only ever added, and every
// added field is optional.
//
// The drawer shows, in this order, as Lens does (capture, 2026-10-11): the Metrics section of a pod
// or node (Metrics; its data is Session.ObjectMetrics), the sections, then the events ("No events found" when there are
// none). The YAML is Cogpit's own view of the object.
//
//	{
//	  "ref": {...}, "title": "Pod: web-0", "status": "ok",
//	  "sections": [
//	    {"title": "Properties", "items": [{"name": "Created", ...}, {"name": "Labels", "fold": "Labels", "chips": [...]}, ...]},
//	    {"title": "Pod Volumes", "items": [...]},
//	    {"title": "app", "group": "Containers", "items": [...]}
//	  ],
//	  "events": [{"type": "Warning", "reason": "BackOff", ...}],
//	  "yaml": "...", "containers": ["app"], "initContainers": ["migrate"]
//	}
type DetailModel struct {
	Ref    model.ObjectRef `json:"ref"`
	Title  string          `json:"title"` // "Pod: web-0"
	Status model.Status    `json:"status,omitempty"`
	// Sections in the drawer's order. The first is always Properties: the object's metadata
	// (Created, Name, Namespace, Labels, Annotations, Controlled By; no UID, as in Lens) followed by
	// the kind's own properties (a pod's Status, Node, Pod IP, ...). The kind's other sections
	// follow (a pod's volumes and containers, a node's Capacity and Allocatable). Item values that
	// name objects carry links; long lists fold behind a count (model.DetailItem).
	Sections []model.DetailSection `json:"sections"`
	// Events are the events whose involved object is this object, newest first, at most 50. The
	// drawer shows them after the sections.
	Events []EventItem `json:"events"`
	// YAML is the object as YAML, managedFields dropped, Secret values redacted.
	YAML string `json:"yaml"`
	// Containers are the pod's container names (logs and shell pick one); empty for other kinds.
	Containers []string `json:"containers,omitempty"`
	// InitContainers are the pod's init container names, which logs and shell can pick too.
	InitContainers []string `json:"initContainers,omitempty"`
	// EphemeralContainers are the pod's ephemeral (debug) container names, which logs, shell and
	// attach can pick too (D-102).
	EphemeralContainers []string `json:"ephemeralContainers,omitempty"`
	// Missing is true when the object is not (or no longer) in the cache.
	Missing bool `json:"missing,omitempty"`
	// Loading is true while the object's kind is still being listed: the object may yet appear,
	// and an EventDetail follows when the list is done.
	Loading bool `json:"loading,omitempty"`
	// NoEvents is true for a kind whose objects no event is about (a virtual kind: Helm
	// releases): the drawer shows no Events section, as Lens's release drawer has none.
	NoEvents bool `json:"noEvents,omitempty"`
	// Metrics is true for a kind with a Metrics section (a pod, a node): the drawer shows it above
	// the Properties, from Session.ObjectMetrics, which follows the open drawer (EventMetrics).
	Metrics bool `json:"metrics,omitempty"`
	// Plain is true for a kind whose drawer is Lens's flat one (a virtual kind: Helm releases,
	// captured 2026-10-12): the title on one line, no Details and YAML tabs, no status pill in the
	// title bar.
	Plain bool `json:"plain,omitempty"`
	// Certificates are the certificates a Secret's values hold, decoded (D-30, secretcerts.go):
	// per key, each certificate's public fields with its times in UTC (RFC 3339), expiresInDays
	// and expired at the moment the model was built. The same fields are the drawer's
	// "Certificates · <key>" sections. Never a value, the PEM text or a private key; empty for
	// other kinds.
	Certificates []model.CertificateKey `json:"certificates,omitempty"`
	// HelmChart is what a Helm chart's drawer adds (helmchartdrawer.go): the version it shows, the
	// versions to pick from, and the state of that version's README, whose text is the "README"
	// section. Nil for other kinds and without chart repositories.
	HelmChart *HelmChartDrawer `json:"helmChart,omitempty"`
}

// EventItem is one event in the drawer.
type EventItem struct {
	Type     string       `json:"type"` // Normal, Warning
	Reason   string       `json:"reason"`
	Message  string       `json:"message"`
	Source   string       `json:"source,omitempty"`
	Count    int64        `json:"count,omitempty"`
	LastSeen int64        `json:"lastSeen,omitempty"` // Unix seconds
	Status   model.Status `json:"status,omitempty"`
}

// Diagnostics are the counters the status bar and the diagnostics view show.
type Diagnostics struct {
	Context   string                `json:"context"`
	Demo      bool                  `json:"demo,omitempty"`
	Requests  cluster.RequestStats  `json:"requests"`
	Informers []cache.InformerStats `json:"informers"`
	// Snapshots counts table snapshots built since start, for the coalescing evidence.
	Snapshots uint64 `json:"snapshots"`
	DemoStats any    `json:"demoStats,omitempty"`
	// Discovery says where the connection's discovery answer came from (D-65); nil for the demo
	// cluster.
	Discovery *cluster.DiscoveryInfo `json:"discovery,omitempty"`
	// Problems counts the lines of the context's diagnostics log seen since its latest connect
	// (D-133): the status bar's button shows it.
	Problems int `json:"problems,omitempty"`
	// Listing says the cache lists every read's initial list since a stream stalled (D-new).
	Listing bool `json:"listing,omitempty"`
}
