package model

import (
	"encoding/json"
	"strconv"
	"time"
)

// Usage is what a pod, a container or a node uses right now, as the Metrics API reports it: CPU
// in millicores and memory in bytes. The zero value is "nothing measured".
type Usage struct {
	CPUMilli    int64 `json:"cpuMilli"`
	MemoryBytes int64 `json:"memoryBytes"`
	// Missing names the parts the provider had no value for: a Prometheus whose CPU rate has no
	// series yet for a node it has memory for (seen once on kind right after connecting). Such a
	// part is unknown, not 0: its field holds 0, its list cell stays empty and a chart leaves the
	// point out. The Metrics Server always measures both.
	Missing UsagePart `json:"missing,omitempty"`
}

// UsagePart is a set of a usage's parts: CPU, memory.
type UsagePart uint8

// The parts of a usage.
const (
	UsageCPU UsagePart = 1 << iota
	UsageMemory
)

// MarshalJSON writes the parts as names: ["cpu"], ["cpu","memory"].
func (p UsagePart) MarshalJSON() ([]byte, error) {
	names := []string{}
	if p&UsageCPU != 0 {
		names = append(names, "cpu")
	}
	if p&UsageMemory != 0 {
		names = append(names, "memory")
	}
	return json.Marshal(names)
}

// UnmarshalJSON reads what MarshalJSON writes; unknown names are ignored.
func (p *UsagePart) UnmarshalJSON(b []byte) error {
	var names []string
	if err := json.Unmarshal(b, &names); err != nil {
		return err
	}
	*p = 0
	for _, n := range names {
		switch n {
		case "cpu":
			*p |= UsageCPU
		case "memory":
			*p |= UsageMemory
		}
	}
	return nil
}

// HasCPU reports whether the CPU was measured.
func (u Usage) HasCPU() bool { return u.Missing&UsageCPU == 0 }

// HasMemory reports whether the memory was measured.
func (u Usage) HasMemory() bool { return u.Missing&UsageMemory == 0 }

// Add returns the sum of two usages. A part is missing from the sum only when both lack it: the
// sum of what was measured is the best number there is.
func (u Usage) Add(o Usage) Usage {
	return Usage{CPUMilli: u.CPUMilli + o.CPUMilli, MemoryBytes: u.MemoryBytes + o.MemoryBytes, Missing: u.Missing & o.Missing}
}

// ContainerUsage is one container's usage.
type ContainerUsage struct {
	Name string `json:"name"`
	Usage
}

// PodUsage is one pod's measurement: its containers' usage at Timestamp, the time the kubelet
// took it (the Metrics API's timestamp, not the time Cogpit asked).
type PodUsage struct {
	Namespace  string           `json:"namespace"`
	Name       string           `json:"name"`
	Timestamp  time.Time        `json:"timestamp"`
	Containers []ContainerUsage `json:"containers"`
}

// Total is the pod's usage: the sum of its containers'. A part is missing only when every
// container lacks it.
func (p PodUsage) Total() Usage {
	if len(p.Containers) == 0 {
		return Usage{}
	}
	u := p.Containers[0].Usage
	for _, c := range p.Containers[1:] {
		u = u.Add(c.Usage)
	}
	return u
}

// NodeUsage is one node's measurement at Timestamp.
type NodeUsage struct {
	Name      string    `json:"name"`
	Timestamp time.Time `json:"timestamp"`
	Usage
}

// NodeDisk is a node's disks as node-exporter reports them through Prometheus, at Timestamp: every
// real filesystem the node mounts (D-90: no memory, kernel or container filesystems, no pods'
// volumes, one mountpoint per device), the root first. Mountpoint, UsedBytes and SizeBytes are
// the fullest of them, the one the Nodes list's Disk column shows. Name is the series' node: its
// node label, else its instance's host, which may be an address (the engine matches it against
// the node's name and addresses).
type NodeDisk struct {
	Name       string    `json:"name"`
	Timestamp  time.Time `json:"timestamp"`
	Mountpoint string    `json:"mountpoint,omitempty"`
	UsedBytes  int64     `json:"usedBytes"`
	SizeBytes  int64     `json:"sizeBytes"`
	// Filesystems are the node's real filesystems: the root ("/") first, then by mountpoint.
	Filesystems []NodeFilesystem `json:"filesystems,omitempty"`
}

// Share is the part of the filesystem in use; 0 for a filesystem without a size.
func (d NodeDisk) Share() float64 { return share(d.UsedBytes, d.SizeBytes) }

// NewNodeDisk is a node's disk of the filesystems fs, in the order given (the root first), its
// summary the fullest of them, the first on a tie.
func NewNodeDisk(name string, at time.Time, fs []NodeFilesystem) NodeDisk {
	d := NodeDisk{Name: name, Timestamp: at, Filesystems: fs}
	for i, f := range fs {
		if i == 0 || f.Share() > d.Share() {
			d.Mountpoint, d.UsedBytes, d.SizeBytes = f.Mountpoint, f.UsedBytes, f.SizeBytes
		}
	}
	return d
}

// NodeFilesystem is one filesystem a node mounts: where, from which device, of which type, its
// size and the bytes in use, the size less what is available (node_filesystem_avail_bytes: what
// an unprivileged process may still write, as df's Avail).
type NodeFilesystem struct {
	Mountpoint string `json:"mountpoint"`
	Device     string `json:"device,omitempty"`
	FSType     string `json:"fstype,omitempty"`
	UsedBytes  int64  `json:"usedBytes"`
	SizeBytes  int64  `json:"sizeBytes"`
}

// Share is the part of the filesystem in use; 0 for a filesystem without a size.
func (f NodeFilesystem) Share() float64 { return share(f.UsedBytes, f.SizeBytes) }

// FreeBytes is what is still available: the size less the bytes in use.
func (f NodeFilesystem) FreeBytes() int64 { return max(f.SizeBytes-f.UsedBytes, 0) }

func share(used, size int64) float64 {
	if size <= 0 {
		return 0
	}
	return float64(used) / float64(size)
}

// CountSample is one point of a count over time: the pods on a group of nodes (the cluster
// Overview's Pods chart).
type CountSample struct {
	At    time.Time `json:"at"`
	Value float64   `json:"value"`
}

// UsageSample is one point of a usage time series.
type UsageSample struct {
	At time.Time `json:"at"`
	Usage
}

// FormatCPU formats millicores as cores the way Lens's lists do, three decimals: 12 → "0.012",
// 1500 → "1.500".
func FormatCPU(milli int64) string {
	return strconv.FormatFloat(float64(milli)/1000, 'f', 3, 64)
}

// FormatPercent formats a share of a whole with one decimal below 10 percent and none above:
// 0.075 → "7.5%", 0.42 → "42%".
func FormatPercent(share float64) string {
	p := share * 100
	if p < 10 {
		return strconv.FormatFloat(p, 'f', 1, 64) + "%"
	}
	return strconv.FormatFloat(p, 'f', 0, 64) + "%"
}

// The usage bars' thresholds (the Nodes list's CPU, Memory and Disk). Cogpit's defaults, not the
// owner's decision: Lens's are not captured. A share at or above UsageWarningShare is drawn in
// the warning colour, at or above UsageErrorShare in the error colour, else in the accent colour.
const (
	UsageWarningShare = 0.8
	UsageErrorShare   = 0.9
)

// UsageBarStatus is a usage bar's colour for a share of the whole: StatusNone below the warning
// share, StatusWarning from it, StatusError from the error share.
func UsageBarStatus(share float64) Status {
	switch {
	case share >= UsageErrorShare:
		return StatusError
	case share >= UsageWarningShare:
		return StatusWarning
	}
	return StatusNone
}

// ShareCell is a usage cell drawn as a bar: the share as text ("7.5%", FormatPercent), its sort
// key in tenths of a percent, and the bar with its threshold colour and detail (the tooltip). A
// negative share counts as 0.
func ShareCell(share float64, detail string) Cell {
	share = max(share, 0)
	return Cell{
		Text: FormatPercent(share), Num: int64(share * 1000),
		Bar: &CellBar{Share: share, Status: UsageBarStatus(share), Detail: detail},
	}
}

// NetworkSample is one point of a pod's or node's network traffic: bytes per second received and
// transmitted, averaged over the provider's rate window. Only a provider with history and network
// metrics (Prometheus) produces it; the network is a pod's, not a container's.
type NetworkSample struct {
	At                  time.Time `json:"at"`
	ReceiveBytesPerSec  float64   `json:"receiveBytesPerSec"`
	TransmitBytesPerSec float64   `json:"transmitBytesPerSec"`
}

// FilesystemSample is one point of a container's, pod's, node's or persistent volume's filesystem:
// the bytes in use when the provider has the usage metric, else (the series' FilesystemSource says
// "io") the read and write rates in bytes per second. A persistent volume's point also has the
// filesystem's size and the bytes still free for writing, as the kubelet reports them
// (kubelet_volume_stats_*). The fields the source does not give are 0.
type FilesystemSample struct {
	At               time.Time `json:"at"`
	UsageBytes       float64   `json:"usageBytes,omitempty"`
	ReadBytesPerSec  float64   `json:"readBytesPerSec,omitempty"`
	WriteBytesPerSec float64   `json:"writeBytesPerSec,omitempty"`
	CapacityBytes    float64   `json:"capacityBytes,omitempty"`
	AvailableBytes   float64   `json:"availableBytes,omitempty"`
}

// Share is the part of a persistent volume's filesystem in use: the bytes in use of its size; 0
// without a size.
func (s FilesystemSample) Share() float64 {
	if s.CapacityBytes <= 0 {
		return 0
	}
	return s.UsageBytes / s.CapacityBytes
}

// VolumeClaim names a bound persistent volume claim and its volume: what a claim's or a volume's
// drawer shows the usage of (KindProfile.Volume). The kubelet labels a volume's usage with its
// claim (namespace, persistentvolumeclaim), so the volume is found by its claim.
type VolumeClaim struct {
	Namespace string `json:"namespace"`
	Claim     string `json:"claim"`
	Volume    string `json:"volume"`
}
